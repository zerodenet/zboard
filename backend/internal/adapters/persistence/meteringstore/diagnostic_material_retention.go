package meteringstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const DiagnosticMaterialMinimumAge = 7 * 24 * time.Hour
const DiagnosticMaterialRetentionLimit = 32
const DiagnosticMaterialReadBudget int64 = 256 * 1024

var ErrDiagnosticMaterial = errors.New("invalid retained diagnostic provenance")

// Called only for a newly inserted legacy signed report in its authority
// transaction. Empty and already compact omission receipts need no body owner.
func enqueueDiagnosticMaterial(tx *gorm.DB, record model.TrafficRecord, original string, now time.Time) error {
	if original == "" || len(original) > nodeReportMetadataByteLimit {
		return nil
	}
	if record.ID == 0 || record.EventType != "" || record.FlowID != "" || record.EntitlementEpoch != 0 || record.Meta != original {
		return ErrDiagnosticMaterial
	}
	sum := sha256.Sum256([]byte(original))
	now = now.UTC().Truncate(time.Millisecond)
	return tx.Create(&model.TrafficDiagnosticMaterial{TrafficRecordID: record.ID, SourceBytes: uint64(len(original)), SourceSHA256: hex.EncodeToString(sum[:]), CreatedAt: now, ReviewAt: now.Add(DiagnosticMaterialMinimumAge)}).Error
}

type DiagnosticMaterialRetention struct {
	DB  *gorm.DB
	Now func() time.Time
}

func (s DiagnosticMaterialRetention) now() time.Time {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	return now.UTC().Truncate(time.Millisecond)
}

func (s DiagnosticMaterialRetention) HasPending(ctx context.Context) (bool, error) {
	if s.DB == nil {
		return false, errors.New("diagnostic retention unavailable")
	}
	var head struct{ TrafficRecordID uint }
	r := s.DB.WithContext(ctx).Model(&model.TrafficDiagnosticMaterial{}).Select("traffic_record_id").Where("retired_at IS NULL AND review_at <= ?", s.now()).Limit(1).Scan(&head)
	return r.RowsAffected > 0, r.Error
}

func (s DiagnosticMaterialRetention) Retire(ctx context.Context, limit int, budget int64) (out metering.DiagnosticMaterialRetentionResult, err error) {
	if s.DB == nil || limit < 1 || limit > DiagnosticMaterialRetentionLimit || budget < nodeReportMetadataByteLimit || budget > DiagnosticMaterialReadBudget {
		return out, errors.New("invalid diagnostic retention budget")
	}
	now := s.now()
	for out.Inspected < limit {
		found, exhausted, retired := false, false, false
		var size uint64
		var materialErr error
		var id uint
		options := &sql.TxOptions{}
		if s.DB.Dialector.Name() == "mysql" {
			options.Isolation = sql.LevelReadCommitted
		}
		e := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var header model.TrafficDiagnosticMaterial
			e := tx.Select("traffic_record_id,CASE WHEN source_bytes BETWEEN 1 AND 65535 THEN source_bytes ELSE 0 END AS source_bytes,substr(source_sha256,1,65) AS source_sha256,retired_at,review_at,created_at").Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("retired_at IS NULL AND review_at <= ?", now).Order("review_at,traffic_record_id").First(&header).Error
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return nil
			}
			if e != nil {
				return e
			}
			found = true
			id = header.TrafficRecordID
			deferReview := func(next time.Time) error {
				return tx.Model(&model.TrafficDiagnosticMaterial{}).Where("traffic_record_id = ? AND retired_at IS NULL", id).UpdateColumns(map[string]any{"review_at": next}).Error
			}
			deferForAge := func(at time.Time) error {
				next := at.Add(DiagnosticMaterialMinimumAge)
				if next.After(now.Add(time.Hour)) {
					next = now.Add(time.Hour)
				}
				return deferReview(next)
			}
			invalid := func(reason string) error {
				materialErr = fmt.Errorf("%w: %s", ErrDiagnosticMaterial, reason)
				return deferReview(now.Add(time.Hour))
			}
			decoded, e := hex.DecodeString(header.SourceSHA256)
			if id == 0 || header.SourceBytes == 0 || header.SourceBytes > nodeReportMetadataByteLimit || len(decoded) != sha256.Size || e != nil || header.CreatedAt.IsZero() {
				return invalid("invalid material header")
			}
			if header.CreatedAt.After(now.Add(-DiagnosticMaterialMinimumAge)) {
				return deferForAge(header.CreatedAt)
			}
			// Read only bounded metadata until the exact source is locked and its
			// byte length verified. Native and unknown historical records are excluded.
			var source struct {
				ID                   uint
				EventType, FlowID    string
				EntitlementEpoch     uint64
				MaterialLength       sql.NullInt64
				CreatedAt, UpdatedAt time.Time
			}
			length := "octet_length(meta)"
			if tx.Dialector.Name() == "mysql" {
				length = "OCTET_LENGTH(meta)"
			}
			e = tx.Model(&model.TrafficRecord{}).Select("id,CASE WHEN event_type = '' THEN '' ELSE 'nonlegacy' END AS event_type,CASE WHEN flow_id = '' THEN '' ELSE 'nonlegacy' END AS flow_id,CASE WHEN entitlement_epoch = 0 THEN 0 ELSE 1 END AS entitlement_epoch,created_at,updated_at,"+length+" AS material_length").Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("id = ?", id).Take(&source).Error
			if errors.Is(e, gorm.ErrRecordNotFound) {
				var n int64
				if e := tx.Model(&model.TrafficRecord{}).Where("id = ?", id).Count(&n).Error; e != nil {
					return e
				}
				if n == 0 {
					return invalid("material source missing")
				}
				return deferReview(now.Add(time.Minute))
			}
			if e != nil {
				return e
			}
			if source.EventType != "" || source.FlowID != "" || source.EntitlementEpoch != 0 || source.CreatedAt.IsZero() || source.UpdatedAt.IsZero() || source.UpdatedAt.Before(source.CreatedAt) || !source.MaterialLength.Valid || source.MaterialLength.Int64 < 1 || uint64(source.MaterialLength.Int64) != header.SourceBytes {
				return invalid("material source ownership or size differs")
			}
			latest := source.CreatedAt
			if source.UpdatedAt.After(latest) {
				latest = source.UpdatedAt
			}
			if latest.After(now.Add(-DiagnosticMaterialMinimumAge)) {
				return deferForAge(latest)
			}
			if header.SourceBytes > uint64(budget) {
				exhausted = true
				return nil
			}
			budget -= int64(header.SourceBytes)
			var body struct{ Meta string }
			bodyExpression := "substr(CAST(meta AS BLOB),1,?) AS meta"
			if tx.Dialector.Name() == "mysql" {
				bodyExpression = "substr(CAST(meta AS BINARY),1,?) AS meta"
			}
			if e := tx.Model(&model.TrafficRecord{}).Select(bodyExpression, nodeReportMetadataByteLimit+1).Where("id = ?", id).Take(&body).Error; e != nil {
				return e
			}
			sum := sha256.Sum256([]byte(body.Meta))
			if uint64(len(body.Meta)) != header.SourceBytes || hex.EncodeToString(sum[:]) != header.SourceSHA256 {
				return invalid("material digest differs")
			}
			receipt := fmt.Sprintf(`{"type":"diagnostic_meta_retired","version":1,"original_bytes":%d,"sha256":"%s","retired_at":"%s"}`, header.SourceBytes, header.SourceSHA256, now.Format(time.RFC3339Nano))
			// MySQL's published ledger has ON UPDATE CURRENT_TIMESTAMP(3).
			// Pin its locked original value as well as skipping GORM hooks: optional
			// diagnostic retirement must not rewrite financial source chronology.
			changed := tx.Model(&model.TrafficRecord{}).Where("id = ?", id).UpdateColumns(map[string]any{"meta": receipt, "updated_at": source.UpdatedAt})
			if changed.Error != nil {
				return changed.Error
			}
			if changed.RowsAffected != 1 {
				return ErrDiagnosticMaterial
			}
			changed = tx.Model(&model.TrafficDiagnosticMaterial{}).Where("traffic_record_id = ? AND retired_at IS NULL", id).UpdateColumns(map[string]any{"retired_at": now})
			if changed.Error != nil {
				return changed.Error
			}
			if changed.RowsAffected != 1 {
				return ErrDiagnosticMaterial
			}
			retired = true
			size = header.SourceBytes
			return nil
		}, options)
		if e != nil {
			return out, errors.Join(err, e)
		}
		if !found || exhausted {
			return out, err
		}
		out.Inspected++
		if retired {
			out.Retired++
			out.Bytes += size
		}
		if materialErr != nil {
			err = errors.Join(err, fmt.Errorf("diagnostic record %d: %w", id, materialErr))
		}
		if ctx.Err() != nil {
			return out, errors.Join(err, ctx.Err())
		}
	}
	return out, err
}
