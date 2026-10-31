package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestSQLiteMeteringReportReceiptsKeepReplayIndependentOfDetail(t *testing.T) {
	assertMeteringReportReceipts(t, newOrderFixture(t))
}
func TestMySQLMeteringReportReceiptsKeepReplayIndependentOfDetail(t *testing.T) {
	assertMeteringReportReceipts(t, newMySQLBusinessOrderFixture(t))
}

func assertMeteringReportReceipts(t *testing.T, f orderFixture) {
	h, c := accountingFixtureFromOrder(t, f)
	events := accountingBenchmarkEvents(c, 0, 1)
	if !datastore.IsSQLite(h.db) {
		events[0].ID = "Résumé-receipt"
	}
	trigger := `CREATE TRIGGER reject_report_receipt BEFORE INSERT ON metering_report_receipts BEGIN SELECT RAISE(ABORT,'receipt persistence unavailable'); END`
	if !datastore.IsSQLite(h.db) {
		trigger = `CREATE TRIGGER reject_report_receipt BEFORE INSERT ON metering_report_receipts FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='receipt persistence unavailable'`
	}
	if err := h.db.Exec(trigger).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.projectZeroNodeEvents(t.Context(), events); err == nil {
		t.Fatal("receipt failure acknowledged as settled")
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 0, subStatusActive)
	for _, table := range []string{"traffic_records", "flow_usages", "metering_usage_projection_pending", "metering_report_receipts", "zero_event_node_cursors"} {
		var n int64
		if err := h.db.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("partial accounting %s: %d %v", table, n, err)
		}
	}
	if err := h.db.Exec("DROP TRIGGER reject_report_receipt").Error; err != nil {
		t.Fatal(err)
	}
	if err := h.projectZeroNodeEvents(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 30, subStatusActive)
	var fact model.TrafficRecord
	if err := h.db.First(&fact).Error; err != nil {
		t.Fatal(err)
	}
	var receipt model.MeteringReportReceipt
	if err := h.db.Where("node_id = ? AND report_id = ?", c.NodeID, events[0].ID).First(&receipt).Error; err != nil || receipt.TrafficRecordID != fact.ID || receipt.ProtocolEndpointID != c.ProtocolEndpointID {
		t.Fatalf("receipt lost settled identity %+v %v", receipt, err)
	}
	before, err := json.Marshal(fact)
	if err != nil {
		t.Fatal(err)
	}
	// A known receipt must answer idempotency even when raw detail cannot be
	// queried. Retain all source rows: this is an unavailable-detail fault, not
	// permission to delete financial history or enable production archival.
	const callback = "test:block_raw_report_replay"
	blocked := 0
	if err := h.db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "traffic_records" {
			blocked++
			tx.AddError(errors.New("raw historical detail unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	replay := events[0]
	replay.FlowID = "different-flow-with-same-report"
	replay.Sequence = 100
	replay.Payload = json.RawMessage(fmt.Sprintf(`{"flow_id":%q,"traffic":{"bytes_up":100,"bytes_down":200}}`, replay.FlowID))
	if !datastore.IsSQLite(h.db) {
		replay.ID = "RESUME-receipt"
	}
	err = h.projectZeroNodeEvents(t.Context(), append(events[:0:0], replay))
	if removeErr := h.db.Callback().Query().Remove(callback); removeErr != nil {
		t.Fatal(removeErr)
	}
	if err != nil || blocked != 0 {
		t.Fatalf("settled replay still depends on raw detail: reads=%d %v", blocked, err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 30, subStatusActive)
	var unchanged model.TrafficRecord
	if err := h.db.First(&unchanged, fact.ID).Error; err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(unchanged)
	if err != nil || string(before) != string(after) || unchanged.Nonce != fact.Nonce || unchanged.EntitlementEpoch != fact.EntitlementEpoch {
		t.Fatal("receipt replay rewrote original billing or proof", err)
	}
	// A genuine pre-0063 fact carries its original report ID. A minute bucket
	// cannot recover erased report identities from its synthetic grouping key.
	// This fixture has exactly one contribution, so model that legacy row before
	// removing only its receipt; amounts and quota remain unchanged.
	if err := h.db.Model(&model.TrafficRecord{}).Where("id = ?", fact.ID).Updates(map[string]any{"report_id": events[0].ID, "nonce": meteringstoreReceiptNonce(events[0].ID), "is_aggregate": false, "event_type": "flow.updated", "flow_id": events[0].FlowID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Where("node_id = ? AND report_id = ?", receipt.NodeID, receipt.ReportID).Delete(&model.MeteringReportReceipt{}).Error; err != nil {
		t.Fatal(err)
	}
	legacyReads := 0
	if err := h.db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "traffic_records" {
			return
		}
		legacyReads++
		selection := strings.ToLower(strings.Join(tx.Statement.Selects, ","))
		if strings.Contains(selection, "meta") || selection == "" || strings.Contains(selection, "*") {
			tx.AddError(errors.New("legacy identity compatibility read loaded diagnostic detail"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	err = h.projectZeroNodeEvents(t.Context(), events)
	if removeErr := h.db.Callback().Query().Remove(callback); removeErr != nil {
		t.Fatal(removeErr)
	}
	if err != nil || legacyReads != 1 {
		t.Fatalf("bounded legacy compatibility failed: reads=%d %v", legacyReads, err)
	}
	if err := h.db.Where("node_id = ? AND report_id = ?", c.NodeID, events[0].ID).First(&receipt).Error; err != nil || receipt.TrafficRecordID != fact.ID {
		t.Fatalf("legacy receipt invented identity %+v %v", receipt, err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 30, subStatusActive)
	drainUsageProjections(t, h.db)
}

func meteringstoreReceiptNonce(report string) string {
	sum := sha256.Sum256([]byte(report))
	return hex.EncodeToString(sum[:])
}
