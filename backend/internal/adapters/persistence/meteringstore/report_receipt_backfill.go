package meteringstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BackfillReportReceipts registers one explicitly bounded historical ID range.
// It never changes a financial fact, re-applies quota, rebuilds statistics or
// deletes source evidence. A returned cursor describes committed work only.
// RangeExhausted is not archive permission or proof against old writers and
// concurrent transactions that have not yet committed a smaller source ID.
func BackfillReportReceipts(ctx context.Context, db *gorm.DB, after, through uint, limit int) (metering.ReportReceiptBackfillResult, error) {
	empty := metering.ReportReceiptBackfillResult{}
	if ctx == nil || db == nil {
		return empty, errors.New("report receipt backfill requires an explicit increasing ID range and a batch between 1 and 200")
	}
	if err := metering.ValidateReportReceiptBackfill(after, through, limit); err != nil {
		return empty, err
	}
	out := metering.ReportReceiptBackfillResult{AfterID: after, ThroughID: through, NextAfterID: after}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var records []model.TrafficRecord
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id,node_id,report_id,protocol_endpoint_id").Where("id > ? AND id <= ?", after, through).Where("(event_type IS NULL OR event_type <> ?)", nativeSettlementType).Order("id").Limit(limit).Find(&records).Error; err != nil {
			return err
		}
		if err := persistReportReceipts(tx, records); err != nil {
			return err
		}
		out.Scanned = len(records)
		for _, record := range records {
			out.NextAfterID = record.ID
			if record.NodeID == 0 || record.ReportID == "" {
				out.Unidentified++
			} else {
				out.IdentitiesVerified++
			}
		}
		out.RangeExhausted = len(records) < limit || out.NextAfterID == through
		if out.RangeExhausted {
			out.NextAfterID = through
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return empty, err
	}
	return out, nil
}
