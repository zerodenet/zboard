package meteringstore

import (
	"errors"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const principalRuntimeReadBudget = 8 * 1024 * 1024

// The caller supplies a fixed owned table and exact key query in the existing
// transaction. Metadata is selected and closed before bounded raw fetch. The
// shared budget covers all generation/current/duplicate reads for one event.
func readPrincipalRuntime(query *gorm.DB, out any, budget *int64) error {
	size := "OCTET_LENGTH(core_instance_id)"
	if query.Dialector.Name() == "sqlite" {
		size = "length(CAST(core_instance_id AS BLOB))"
	}
	var head struct {
		BodyBytes   int64
		DigestBytes int64
	}
	selection := size + " AS body_bytes"
	digestSize := ""
	if _, ok := out.(*PrincipalFlowObservation); ok {
		digestSize = "OCTET_LENGTH(core_instance_sha256)"
		if query.Dialector.Name() == "sqlite" {
			digestSize = "length(CAST(core_instance_sha256 AS BLOB))"
		}
		selection += "," + digestSize + " AS digest_bytes"
	}
	if err := query.Session(&gorm.Session{}).Select(selection).Take(&head).Error; err != nil {
		return err
	}
	if _, ok := out.(*PrincipalFlowObservation); ok && head.DigestBytes != 64 {
		return errors.New("principal runtime digest evidence is incomplete or oversized")
	}

	if head.BodyBytes < 0 || head.BodyBytes > metering.MaxRuntimeIdentityBytes || head.BodyBytes+head.DigestBytes > *budget {
		return errors.New("principal runtime identity evidence exceeds read budget; observation remains unknown")
	}
	*budget -= head.BodyBytes + head.DigestBytes
	// Recheck length in the same exact-key statement; concurrent replacement may
	// not cause an unchecked larger fetch, even outside an UPDATE-capable driver.
	bounded := query.Session(&gorm.Session{}).Where(size+" = ?", head.BodyBytes)
	if digestSize != "" {
		bounded = bounded.Where(digestSize+" = ?", head.DigestBytes)
	}
	return bounded.Take(out).Error
}

func verifyPrincipalDuplicate(tx *gorm.DB, row PrincipalFlowObservation, budget *int64) error {
	var heads []struct{ ID uint64 }
	seen := map[uint64]bool{}
	// Two exact owned unique identities, each a current locking read. Do not
	// resolve a1062 against an older repeatable-read consistent snapshot.
	for _, query := range []*gorm.DB{
		tx.Model(&PrincipalFlowObservation{}).Where("node_id = ? AND event_id = ?", row.NodeID, row.EventID),
		tx.Model(&PrincipalFlowObservation{}).Where("node_id = ? AND core_instance_sha256 = ? AND session_registry_revision = ? AND principal_key = ?", row.NodeID, row.CoreInstanceSHA256, row.SessionRegistryRevision, row.PrincipalKey),
	} {
		var head struct{ ID uint64 }
		result := query.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Limit(1).Find(&head)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 && !seen[head.ID] {
			seen[head.ID] = true
			heads = append(heads, head)
		}
	}

	if len(heads) == 0 {
		return errors.New("principal duplicate did not resolve to a retained identity")
	}
	for _, head := range heads {
		var existing PrincipalFlowObservation
		if err := readPrincipalRuntime(tx.Model(&PrincipalFlowObservation{}).Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ?", head.ID), &existing, budget); err != nil {
			return err
		}
		if existing.CoreInstanceSHA256 != metering.RuntimeIdentityDigest(existing.CoreInstanceID) {
			return errors.New("retained principal runtime digest does not match complete raw identity")
		}
		if existing.CoreInstanceID != row.CoreInstanceID || existing.PrincipalKey != row.PrincipalKey || existing.SessionRegistryRevision != row.SessionRegistryRevision {
			return fmt.Errorf("principal event or runtime digest collision at observation %d; retained original evidence", head.ID)
		}
	}
	return nil
}
