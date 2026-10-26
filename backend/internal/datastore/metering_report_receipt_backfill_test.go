package datastore_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestSQLiteMeteringReportReceiptBoundedBackfill(t *testing.T) { assertReceiptBackfill(t, false) }
func TestMySQLMeteringReportReceiptBoundedBackfill(t *testing.T)  { assertReceiptBackfill(t, true) }
func assertReceiptBackfill(t *testing.T, mysql bool) {
	db := diagnosticSchemaDB(t, mysql)
	first := diagnosticSchemaSource(t, db, false)
	facts := make([]model.TrafficRecord, 204)
	for i := range facts {
		facts[i] = first
		facts[i].ID = 0
		facts[i].ReportID = fmt.Sprintf("backfill-%03d", i)
		facts[i].Nonce = facts[i].ReportID
	}
	if err := db.CreateInBatches(&facts, 100).Error; err != nil {
		t.Fatal(err)
	}
	through := facts[len(facts)-1].ID
	var beforeFacts, beforeReceipts int64
	if err := db.Model(&model.TrafficRecord{}).Count(&beforeFacts).Error; err != nil {
		t.Fatal(err)
	}
	// Block loading diagnostic bodies while registering historical identities.
	const callback = "backfill-forbid-body"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "traffic_records" {
			return
		}
		if len(tx.Statement.Selects) == 0 || strings.Contains(strings.ToLower(strings.Join(tx.Statement.Selects, ",")), "meta") {
			tx.AddError(fmt.Errorf("historical diagnostic body loaded"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(callback)
	firstResult, err := application.BackfillMeteringReportReceipts(t.Context(), db, 0, through, 70)
	if err != nil || firstResult.Scanned != 70 || firstResult.IdentitiesVerified != 70 || firstResult.RangeExhausted {
		t.Fatal(firstResult, err)
	}
	retry, err := meteringstore.BackfillReportReceipts(t.Context(), db, 0, through, 70)
	if err != nil || retry != firstResult {
		t.Fatal("committed range replay changed cursor or outcome", retry, err)
	}
	if err := db.Model(&model.MeteringReportReceipt{}).Count(&beforeReceipts).Error; err != nil || beforeReceipts != 70 {
		t.Fatal("replay duplicated identity", beforeReceipts, err)
	}
	// Insert a conflicting identity in the next range. Earlier identities from
	// that attempted batch must roll back and no usable cursor may escape.
	conflict := model.MeteringReportReceipt{NodeID: first.NodeID, ReportID: facts[100].ReportID, ProtocolEndpointID: first.ProtocolEndpointID, TrafficRecordID: first.ID, CreatedAt: first.CreatedAt}
	if err := db.Create(&conflict).Error; err != nil {
		t.Fatal(err)
	}
	result, err := meteringstore.BackfillReportReceipts(t.Context(), db, firstResult.NextAfterID, through, 70)
	if err == nil || result != (metering.ReportReceiptBackfillResult{}) {
		t.Fatal("failed batch returned a committed cursor", result, err)
	}
	var count int64
	if err := db.Model(&model.MeteringReportReceipt{}).Count(&count).Error; err != nil || count != 71 {
		t.Fatal("partial backfill escaped rollback", count, err)
	}
	// This is the explicitly injected corrupt fixture, not business cleanup.
	if err := db.Where("node_id = ? AND report_id = ?", conflict.NodeID, conflict.ReportID).Delete(&model.MeteringReportReceipt{}).Error; err != nil {
		t.Fatal(err)
	}
	cursor := firstResult.NextAfterID
	for i := 0; i < 3; i++ {
		result, err = meteringstore.BackfillReportReceipts(t.Context(), db, cursor, through, 70)
		if err != nil || result.Scanned > 70 || result.NextAfterID <= cursor {
			t.Fatal(result, err)
		}
		cursor = result.NextAfterID
		if result.RangeExhausted {
			break
		}
	}
	if cursor != through || !result.RangeExhausted {
		t.Fatal("finite window did not finish", result)
	}
	if err := db.Model(&model.MeteringReportReceipt{}).Count(&count).Error; err != nil || count != 205 {
		t.Fatal("missing bounded-window identities", count, err)
	}
	if err := db.Callback().Query().Remove(callback); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.TrafficRecord{}).Count(&count).Error; err != nil || count != beforeFacts {
		t.Fatal("backfill changed financial fact count", count, err)
	}
	var kept model.TrafficRecord
	if err := db.First(&kept, first.ID).Error; err != nil || kept.Meta != first.Meta || kept.UsedBytes != first.UsedBytes || kept.RawBytes != first.RawBytes || kept.Nonce != first.Nonce {
		t.Fatal("backfill rewrote source evidence", err)
	}
	// A later source lies outside the explicit upper bound and stays untouched.
	later := first
	later.ID = 0
	later.ReportID = "after-window"
	later.Nonce = "after-window"
	if err := db.Create(&later).Error; err != nil {
		t.Fatal(err)
	}
	result, err = meteringstore.BackfillReportReceipts(t.Context(), db, through-1, through, 70)
	if err != nil || result.Scanned != 1 || !result.RangeExhausted {
		t.Fatal(result, err)
	}
	if err := db.Model(&model.MeteringReportReceipt{}).Where("traffic_record_id = ?", later.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("backfill crossed requested upper bound", count, err)
	}
	assertReportReceiptConcurrentRange(t, db, through, later)
	if err := db.Exec("ALTER TABLE traffic_records RENAME COLUMN id TO changed_source_id").Error; err != nil {
		t.Fatal(err)
	}
	if result, err := application.BackfillMeteringReportReceipts(t.Context(), db, 0, later.ID, 70); err == nil || result != (metering.ReportReceiptBackfillResult{}) {
		t.Fatal("changed source identity admitted by the application boundary", result, err)
	}
	if err := db.Model(&model.MeteringReportReceipt{}).Count(&count).Error; err != nil || count != 206 {
		t.Fatal("source-schema refusal changed committed identities", count, err)
	}
	if err := db.Exec("ALTER TABLE traffic_records RENAME COLUMN changed_source_id TO id").Error; err != nil {
		t.Fatal(err)
	}
}
