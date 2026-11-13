package meteringstore

import (
	"errors"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const retainedSettlementType = "settlement.retained"
const retainedSettlementPrefix = "retained:v1:"

func recordForReceipt(tx *gorm.DB, receipt model.MeteringReportReceipt, eventID string) (model.TrafficRecord, error) {
	var record model.TrafficRecord
	recordID, resultID := receipt.TrafficRecordID, uint(0)
	for hop := 0; hop < 4; hop++ {
		var link model.MeteringLedgerTarget
		found, err := loadLedgerTarget(tx, receipt.NodeID, recordID, &link)
		if err != nil {
			return record, err
		}
		if !found {
			break
		}
		if link.NodeID != receipt.NodeID || link.ProtocolEndpointID != receipt.ProtocolEndpointID || (link.RecordID == 0) == (link.ResultID == 0) {
			return record, ErrReportReceiptConflict
		}
		if link.ResultID != 0 {
			resultID = link.ResultID
			break
		}
		recordID = link.RecordID
		if hop == 3 {
			return record, ErrReportReceiptConflict
		}
	}
	if resultID != 0 {
		var r model.MeteringSettlementResult
		if err := tx.First(&r, resultID).Error; err != nil {
			return record, errors.Join(ErrReportReceiptConflict, err)
		}
		if r.NodeID != receipt.NodeID || r.ProtocolEndpointID != receipt.ProtocolEndpointID {
			return record, ErrReportReceiptConflict
		}
		return model.TrafficRecord{ID: receipt.TrafficRecordID, ResultRecorded: true, RetentionLevel: 3, UserID: r.UserID, SubscriptionID: r.SubscriptionID, EntitlementEpoch: r.EntitlementEpoch, NodeID: r.NodeID, ProtocolEndpointID: r.ProtocolEndpointID, TrafficCalcMode: r.TrafficCalcMode, ProtocolMultiplierMilli: r.ProtocolMultiplierMilli, RawBytes: r.RawBytes, UploadBytes: r.UploadBytes, DownloadBytes: r.DownloadBytes, UsedBytes: r.UsedBytes, RecordCount: r.RecordCount, IsAggregate: true, EventType: "settlement.result", Meta: `{"kind":"settlement_result","original_detail_expired":true}`}, nil
	}
	err := tx.Where("id=? AND node_id=? AND protocol_endpoint_id=?", recordID, receipt.NodeID, receipt.ProtocolEndpointID).Where("is_aggregate=? OR report_id=?", true, eventID).First(&record).Error
	if err != nil {
		return record, errors.Join(ErrReportReceiptConflict, err)
	}
	if record.IsAggregate && !validAggregateRecord(record) {
		return record, ErrReportReceiptConflict
	}
	return record, nil
}
func validAggregateRecord(r model.TrafficRecord) bool {
	if r.Nonce != completionNonce(r.ReportID) {
		return false
	}
	if r.EventType == nativeSettlementType {
		return strings.HasPrefix(r.ReportID, nativeSettlementPrefix) && r.RetentionLevel == 0
	}
	return r.EventType == retainedSettlementType && strings.HasPrefix(r.ReportID, retainedSettlementPrefix) && r.RetentionLevel > 0 && r.RetentionLevel < 3
}

func retainLegacyReportIdentity(tx *gorm.DB, r model.TrafficRecord) error {
	if r.IsAggregate || r.NodeID == 0 || r.ReportID == "" {
		return nil
	}
	row := model.MeteringReportReceipt{NodeID: r.NodeID, ReportID: r.ReportID, ProtocolEndpointID: r.ProtocolEndpointID, TrafficRecordID: r.ID}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return err
	}
	var existing model.MeteringReportReceipt
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id=? AND report_id=?", r.NodeID, r.ReportID).First(&existing).Error; err != nil {
		return err
	}
	if existing.TrafficRecordID != r.ID || existing.ProtocolEndpointID != r.ProtocolEndpointID {
		return ErrReportReceiptConflict
	}
	nonce := model.MeteringNonceReceipt{NodeID: r.NodeID, Nonce: r.Nonce, ReportID: r.ReportID}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&nonce).Error; err != nil {
		return err
	}
	var matching model.MeteringNonceReceipt
	q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id=? AND nonce=? AND report_id=?", r.NodeID, r.Nonce, r.ReportID).First(&matching)
	if q.Error != nil {
		return errors.Join(ErrReportReceiptConflict, q.Error)
	}
	return nil
}
