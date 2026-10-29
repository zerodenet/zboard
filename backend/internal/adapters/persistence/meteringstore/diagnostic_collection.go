package meteringstore

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func diagnosticOmission(meta, reason string) string {
	sum := sha256.Sum256([]byte(meta))
	return fmt.Sprintf(`{"type":"diagnostic_meta_omitted","version":1,"reason":"%s","original_bytes":%d,"max_bytes":%d,"sha256":"%x"}`, reason, len(meta), nodeReportMetadataByteLimit, sum)
}

// Never persist an optional raw body to a database page/WAL before its
// explicit administrator policy and global capacity have been accepted.
func initialDiagnosticMetadata(meta string) string {
	if meta == "" || len(meta) > nodeReportMetadataByteLimit {
		return boundedNodeReportMetadata(meta)
	}
	return diagnosticOmission(meta, "collection_disabled")
}

// Called inside the original report transaction. A current locked policy read
// serializes optional body admissions across different nodes and processes.
// Existing pending identities use a locking read too: a repeatable-read snapshot
// taken before this lock could otherwise admit both contenders to the last slot.
func collectDiagnosticMaterial(tx *gorm.DB, meta string) (string, bool, error) {
	if meta == "" || len(meta) > nodeReportMetadataByteLimit {
		return boundedNodeReportMetadata(meta), false, nil
	}
	var setting struct {
		ID                 uint
		Value, ValueType   string
		IsPublic, IsSecret bool
		UpdatedAt          time.Time
	}
	err := tx.Model(&model.SystemConfig{}).Select("id,substr(value,1,1025) AS value,value_type,is_public,is_secret,updated_at").Clauses(clause.Locking{Strength: "UPDATE"}).Where("config_key = ?", metering.DiagnosticCollectionKey).Take(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return diagnosticOmission(meta, "collection_disabled"), false, nil
	}
	if err != nil {
		return "", false, err
	}
	now := time.Now().UTC() // refresh after the potentially blocking current policy lock
	policy, err := metering.ParseDiagnosticCollection(setting.Value)
	if err != nil || setting.ValueType != "json" || setting.IsPublic || setting.IsSecret {
		return diagnosticOmission(meta, "collection_policy_invalid"), false, nil
	}
	if policy == nil || !policy.Active(now, setting.UpdatedAt) {
		return diagnosticOmission(meta, "collection_disabled"), false, nil
	}
	var pending []uint
	err = tx.Model(&model.TrafficDiagnosticMaterial{}).Clauses(clause.Locking{Strength: "UPDATE"}).Where("retired_at IS NULL").Order("review_at,traffic_record_id").Limit(metering.DiagnosticCollectionSlots).Pluck("traffic_record_id", &pending).Error
	if err != nil {
		return "", false, err
	}
	if len(pending) >= metering.DiagnosticCollectionSlots {
		return diagnosticOmission(meta, "collection_capacity"), false, nil
	}
	return meta, true, nil
}
