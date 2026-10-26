package meteringstore

import (
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrReportReceiptConflict = errors.New("metering report receipt conflicts with its committed ledger identity")

// All current ledger writers already cross RecordUsageProjections in the same
// authority transaction. A receipt failure must roll that transaction back;
// nobody acknowledges an in-memory or separately committed identity.
func persistReportReceipts(tx *gorm.DB, records []model.TrafficRecord) error {
	rows := make([]model.MeteringReportReceipt, 0, len(records))
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, record := range records {
		if record.NodeID == 0 || record.ReportID == "" {
			continue
		}
		if record.ID == 0 {
			return errors.New("report receipt requires a persisted ledger ID")
		}
		rows = append(rows, model.MeteringReportReceipt{NodeID: record.NodeID, ReportID: record.ReportID, ProtocolEndpointID: record.ProtocolEndpointID, TrafficRecordID: record.ID, CreatedAt: now})
	}
	if len(rows) == 0 {
		return nil
	}
	// Never ignore a new fact's conflict. Its quota and pending projection must
	// roll back if a prior committed identity already owns this report ID.
	return tx.CreateInBatches(&rows, 100).Error
}

// A bounded compatibility read establishes only an existing committed ledger
// identity; it does not infer payment, freshness, delivery or historical raw
// metadata. There is no startup backfill or historical full-table scan.
func readReportReceipts(tx *gorm.DB, nodeID uint, ids []string) ([]model.MeteringReportReceipt, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > 200 {
		return nil, errors.New("report identity lookup exceeds bounded batch")
	}
	var rows []model.MeteringReportReceipt
	if err := tx.Where("node_id = ? AND report_id IN ?", nodeID, ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(ids) == 1 && len(rows) == 1 {
		return rows, nil
	}
	known := make(map[string]bool, len(rows))
	for _, row := range rows {
		known[row.ReportID] = true
	}
	wanted := ids
	if datastore.IsSQLite(tx) {
		wanted = make([]string, 0, len(ids))
		for _, id := range ids {
			if !known[id] {
				wanted = append(wanted, id)
			}
		}
	}
	if len(wanted) == 0 {
		return rows, nil
	}
	var legacy []model.TrafficRecord
	// The existing unique node/report index resolves old collation semantics.
	// No Meta, amount, per-connection detail or timestamps are loaded here.
	if err := tx.Select("id,node_id,report_id,protocol_endpoint_id").Where("node_id = ? AND report_id IN ?", nodeID, wanted).Find(&legacy).Error; err != nil {
		return nil, err
	}
	byLedger := make(map[uint]bool, len(rows))
	for _, row := range rows {
		byLedger[row.TrafficRecordID] = true
	}
	for _, record := range legacy {
		if byLedger[record.ID] {
			continue
		}
		candidate := model.MeteringReportReceipt{NodeID: record.NodeID, ReportID: record.ReportID, ProtocolEndpointID: record.ProtocolEndpointID, TrafficRecordID: record.ID, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&candidate)
		if result.Error != nil {
			return nil, result.Error
		}
		var stored model.MeteringReportReceipt
		// Current locking read matters under an existing MySQL RR snapshot when
		// another backfiller won the same identity while this insertion waited.
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("node_id = ? AND report_id = ?", nodeID, record.ReportID).Take(&stored).Error; err != nil {
			return nil, err
		}
		if stored.TrafficRecordID != record.ID || stored.ProtocolEndpointID != record.ProtocolEndpointID {
			return nil, ErrReportReceiptConflict
		}
		rows = append(rows, stored)
		byLedger[record.ID] = true
	}
	return rows, nil
}
