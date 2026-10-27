package observationstore

import (
	"database/sql"
	"errors"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const retirementHashStateLimit = 256
const retirementSummaryLimit = 8192

// Metadata comes first, including for reads that hold the original UPDATE lock.
// SQLite TEXT/BLOB types have no physical size restriction; do not trust DDL or
// load a hostile checkpoint before testing its byte length. The complete fetch
// repeats exact lengths and upper bounds to cover an unlocked concurrent change.
func readRetirement(tx *gorm.DB, id string, lock bool) (out model.NodeObservationRetirement, err error) {
	mode := ""
	if lock {
		mode = "UPDATE"
	}
	return readRetirementMode(tx, id, mode)
}
func readRetirementMode(tx *gorm.DB, id, mode string) (out model.NodeObservationRetirement, err error) {
	hashLength, summaryLength := "OCTET_LENGTH(hash_state)", "OCTET_LENGTH(source_summary)"
	if tx.Dialector.Name() == "sqlite" {
		hashLength = "length(CAST(hash_state AS BLOB))"
		summaryLength = "length(CAST(source_summary AS BLOB))"
	}
	var size struct{ HashBytes, SummaryBytes sql.NullInt64 }
	q := tx.Model(&model.NodeObservationRetirement{}).Select(hashLength+" AS hash_bytes,"+summaryLength+" AS summary_bytes").Where("intent_id = ?", id)
	if mode != "" {
		q = q.Clauses(clause.Locking{Strength: mode})
	}
	if e := q.Take(&size).Error; e != nil {
		return out, e
	}
	if !size.HashBytes.Valid || !size.SummaryBytes.Valid || size.HashBytes.Int64 < 0 || size.HashBytes.Int64 > retirementHashStateLimit || size.SummaryBytes.Int64 < 0 || size.SummaryBytes.Int64 > retirementSummaryLimit {
		return out, invalid("retirement checkpoint or receiver summary exceeds byte boundary")
	}
	// Other SQLite TEXT fields are also returned through bounded expressions;
	// semantic identity/state checks must reject their over-limit truncations.
	fields := "intent_id,substr(header_sha256,1,65) AS header_sha256,substr(state,1,25) AS state,substr(cause,1,65) AS cause,source_summary,next_part,verified_bytes,hash_state,delete_part,remaining_bytes,content_verified,review_at,created_at,updated_at"
	full := tx.Model(&model.NodeObservationRetirement{}).Select(fields).Where("intent_id = ? AND "+hashLength+" = ? AND "+summaryLength+" = ? AND "+hashLength+" <= ? AND "+summaryLength+" <= ?", id, size.HashBytes.Int64, size.SummaryBytes.Int64, retirementHashStateLimit, retirementSummaryLimit)
	if mode != "" {
		full = full.Clauses(clause.Locking{Strength: mode})
	}
	if e := full.Take(&out).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return out, invalid("retirement checkpoint changed after length metadata")
		}
		return out, e
	}
	return out, nil
}
