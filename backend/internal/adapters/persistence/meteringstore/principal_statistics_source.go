package meteringstore

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"strconv"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const PrincipalStatisticsBatchLimit = 200
const principalStatisticsMaterialBudget int64 = 8 * 1024 * 1024
const principalStatisticsRowBudget int64 = metering.MaxRuntimeIdentityBytes + 1024

// Versioned length-delimited original source facts. ID and CreatedAt are stored
// separately, not hashed: replaying the same event must not invent a new receipt
// solely because a retry arrived later. No accounting/credential data changes.
func principalScopeDigest(row PrincipalFlowScopeObservation) string {
	h := sha256.New()
	part := func(v string) { writePrincipalDigestPart(h, v) }
	for _, v := range []string{"zboard-principal-scope-v1", row.ScopeType, strconv.FormatUint(uint64(row.ScopeID), 10), strconv.FormatUint(row.ActiveFlows, 10), strconv.FormatUint(uint64(row.NodeID), 10), row.CoreInstanceID, strconv.FormatUint(row.SessionRegistryRevision, 10), row.EventID, row.Source, row.ObservedAt.UTC().Format(time.RFC3339Nano)} {
		part(v)
	}
	return hex.EncodeToString(h.Sum(nil))
}
func writePrincipalDigestPart(h hash.Hash, v string) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(len(v)))
	_, _ = h.Write(b[:])
	_, _ = h.Write([]byte(v))
}

func canonicalPrincipalScopeTime(tx *gorm.DB, at time.Time) (time.Time, error) {
	at = at.UTC()
	if tx.Dialector.Name() != "mysql" || at.Nanosecond()%int(time.Millisecond) == 0 {
		return at, nil
	}
	// Respect the server's actual fractional-time rounding/truncation mode. Do
	// not change day-boundary semantics by assuming a client-side truncation.
	var row struct{ ObservedAt time.Time }
	if err := tx.Raw("SELECT CAST(? AS DATETIME(3)) AS observed_at", at).Scan(&row).Error; err != nil {
		return time.Time{}, err
	}
	return row.ObservedAt.UTC(), nil
}

func persistPrincipalScopeObservation(tx *gorm.DB, row *PrincipalFlowScopeObservation, known *bool) error {
	if row == nil || row.ScopeID == 0 || row.EventID == "" || len(row.EventID)+len(row.CoreInstanceID)+len(row.Source)+len(row.ScopeType) > int(principalStatisticsRowBudget) || len(row.CoreInstanceID) > metering.MaxRuntimeIdentityBytes || len(row.Source) > 32 || (row.ScopeType != ScopeUser && row.ScopeType != ScopeSubscription) {
		return errors.New("principal statistics source identity is invalid")
	}
	at, err := canonicalPrincipalScopeTime(tx, row.ObservedAt)
	if err != nil {
		return err
	}
	row.ObservedAt = at
	digest := principalScopeDigest(*row)
	duplicate := false
	if tx.Dialector.Name() == "mysql" {
		err = tx.Create(row).Error
		if err != nil {
			var typed *mysqldriver.MySQLError
			if !errors.As(err, &typed) || typed.Number != 1062 {
				return err
			}
			duplicate = true
		}
	} else {
		created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
		if created.Error != nil {
			return created.Error
		}
		duplicate = created.RowsAffected == 0
	}
	if duplicate {
		budget := principalStatisticsMaterialBudget
		query := tx.Model(&PrincipalFlowScopeObservation{}).Clauses(clause.Locking{Strength: "SHARE"}).Where("scope_type = ? AND scope_id = ? AND event_id = ?", row.ScopeType, row.ScopeID, row.EventID)
		existing, err := readPrincipalScopeSource(query, &budget)
		if err != nil {
			return err
		}
		if principalScopeDigest(existing) != digest {
			return errors.New("principal raw scope duplicate conflicts with original source")
		}
		*row = existing
	} else if tx.Dialector.Name() == "mysql" {
		var stored struct {
			ObservedAt time.Time
			CreatedAt  time.Time
		}
		if err := tx.Model(&PrincipalFlowScopeObservation{}).Select("observed_at,created_at").Where("id = ?", row.ID).Take(&stored).Error; err != nil {
			return err
		}
		row.ObservedAt, row.CreatedAt = stored.ObservedAt.UTC(), stored.CreatedAt.UTC()
	}
	// Take pending ownership BEFORE any sample current/gap read. Projectors own
	// pending before sample; the opposite order deadlocks a duplicate at RR.
	if err := EnqueuePrincipalStatistics(tx, *row); err != nil {
		return err
	}
	digest = principalScopeDigest(*row)
	var sample model.PrincipalStatisticsSample
	result := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select(principalStatisticsSampleColumns).Where("scope_type = ? AND scope_id = ? AND event_id = ?", row.ScopeType, row.ScopeID, row.EventID).Limit(1).Find(&sample)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		if sample.SourceDigest != digest || sample.ScopeType != row.ScopeType || sample.ScopeID != row.ScopeID || sample.ActiveFlows != row.ActiveFlows || !sample.ObservedAt.Equal(row.ObservedAt) {
			return errors.New("principal scope event conflicts with retained statistical receipt")
		}
		eventMatches, err := principalStoredEventMatches(tx, sample.ObservationID, row.EventID)
		if err != nil {
			return err
		}
		if !eventMatches {
			return errors.New("retained principal scope event key changed")
		}
		if sample.ObservationID != row.ID {
			if duplicate {
				return errors.New("unrecognized existing principal scope alias retained for recovery")
			}
			// Plain INSERT proved this transaction alone created the temporary replay
			// row. No committed/unknown source is cleared or treated as old diagnostics.
			deleted := tx.Where("id = ?", row.ID).Delete(&PrincipalFlowScopeObservation{})
			if deleted.Error != nil {
				return deleted.Error
			}
			if deleted.RowsAffected != 1 {
				return errors.New("temporary principal replay row ownership lost")
			}
		}
		if err := tx.Where("observation_id = ?", row.ID).Delete(&model.PrincipalStatisticsPending{}).Error; err != nil {
			return err
		}
		*known = true
		return nil
	}
	*known = duplicate
	return nil
}

// Call only with a successfully persisted original source, in its transaction.
// A thin pending intent survives independently of worker/projection availability.
func EnqueuePrincipalStatistics(tx *gorm.DB, row PrincipalFlowScopeObservation) error {
	if tx == nil || row.ID == 0 || row.ScopeID == 0 || row.ObservedAt.IsZero() {
		return errors.New("principal projection requires a persisted scope source")
	}
	var actual struct {
		ScopeType  string
		ScopeID    uint
		ObservedAt time.Time
	}
	if err := tx.Model(&PrincipalFlowScopeObservation{}).Clauses(clause.Locking{Strength: "SHARE"}).Select("scope_type,scope_id,observed_at").Where("id = ?", row.ID).Take(&actual).Error; err != nil {
		return err
	}
	if actual.ScopeType != row.ScopeType || actual.ScopeID != row.ScopeID {
		return errors.New("principal pending input is not its original persisted scope")
	}
	pending := model.PrincipalStatisticsPending{ObservationID: row.ID, ScopeType: actual.ScopeType, ScopeID: actual.ScopeID, ObservedAt: actual.ObservedAt.UTC(), CreatedAt: time.Now().UTC()}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&pending).Error; err != nil {
		return err
	}
	var stored model.PrincipalStatisticsPending
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("observation_id = ?", row.ID).Take(&stored).Error; err != nil {
		return err
	}
	if stored.ScopeType != pending.ScopeType || stored.ScopeID != pending.ScopeID || !stored.ObservedAt.Equal(pending.ObservedAt) {
		return errors.New("principal projection pending identity conflicts")
	}
	return nil
}

func principalScopeLengths(db *gorm.DB) (string, string) {
	length := func(v string) string {
		if db.Dialector.Name() == "sqlite" {
			return "length(CAST(" + v + " AS BLOB))"
		}
		return "OCTET_LENGTH(" + v + ")"
	}
	return length("core_instance_id"), strings.Join([]string{length("core_instance_id"), length("event_id"), length("scope_type"), length("source")}, "+")
}
func readPrincipalScopeSource(query *gorm.DB, budget *int64) (PrincipalFlowScopeObservation, error) {
	core, material := principalScopeLengths(query)
	var head struct {
		CoreBytes     int64
		MaterialBytes int64
	}
	if err := query.Session(&gorm.Session{}).Select(core + " AS core_bytes," + material + " AS material_bytes").Take(&head).Error; err != nil {
		return PrincipalFlowScopeObservation{}, err
	}
	if head.CoreBytes < 0 || head.CoreBytes > metering.MaxRuntimeIdentityBytes || head.MaterialBytes < 0 || head.MaterialBytes > principalStatisticsRowBudget || head.MaterialBytes > *budget {
		return PrincipalFlowScopeObservation{}, errors.New("principal scope evidence exceeds bounded material budget")
	}
	*budget -= head.MaterialBytes
	var row PrincipalFlowScopeObservation
	if err := query.Session(&gorm.Session{}).Where(core+" = ? AND "+material+" = ?", head.CoreBytes, head.MaterialBytes).Take(&row).Error; err != nil {
		return row, err
	}
	return row, nil
}
