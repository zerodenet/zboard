package handler

import (
	"errors"
	"testing"
	"time"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	gormmysql "gorm.io/driver/mysql"
)

func TestMySQLMeteringReportReceiptConflictCannotUseClientFoundRows(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	if err := h.projectZeroNodeEvents(t.Context(), accountingBenchmarkEvents(c, 0, 1)); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 30, subStatusActive)
	var original model.TrafficRecord
	if err := h.db.First(&original).Error; err != nil {
		t.Fatal(err)
	}
	// Pre-0078 receipt compatibility: a legacy row must still bind its own
	// original report ID. Canonical minute rows intentionally share IDs across
	// many legitimate receipts, so use the published legacy layout explicitly.
	original.ReportID = accountingBenchmarkEvents(c, 0, 1)[0].ID
	original.Nonce = meteringstoreReceiptNonce(original.ReportID)
	if err := h.db.Model(&model.TrafficRecord{}).Where("id=?", original.ID).UpdateColumns(map[string]any{"is_aggregate": false, "record_count": 1, "report_id": original.ReportID, "nonce": original.Nonce, "event_type": "flow.updated", "flow_id": "legacy-report-conflict"}).Error; err != nil {
		t.Fatal(err)
	}
	// Explicit corruption fixture: a different report is falsely mapped to an
	// existing financial row. The accounting transaction must reject it rather
	// than accepting a no-op duplicate's CLIENT_FOUND_ROWS success count.
	const conflictID = "client-found-rows-foreign-identity"
	bad := model.MeteringReportReceipt{NodeID: c.NodeID, ReportID: conflictID, ProtocolEndpointID: c.ProtocolEndpointID, TrafficRecordID: original.ID, CreatedAt: time.Now().UTC()}
	if err := h.db.Create(&bad).Error; err != nil {
		t.Fatal(err)
	}
	dialect, ok := h.db.Dialector.(*gormmysql.Dialector)
	if !ok {
		t.Fatal("actual MySQL dialector required")
	}
	cfg, err := mysqlconfig.ParseDSN(dialect.Config.DSN)
	if err != nil {
		t.Fatal("isolated test connection metadata invalid")
	}
	cfg.ClientFoundRows = true
	db, err := datastore.Open(cfg.FormatDSN())
	if err != nil {
		t.Fatal("client-found-rows test connection failed")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	service := meteringstore.CompletionAccounting{DB: db, Cipher: h.credentialCipher}
	input := metering.CompletedFlow{NodeID: c.NodeID, PrincipalKey: c.PrincipalKey, EventID: conflictID, CoreInstanceID: "conflict-runtime", FlowID: "conflict-flow", EventType: "flow.completed", Sequence: 1, Revision: 1, BytesUp: 10, BytesDown: 20, OccurredAt: time.Now().UTC()}
	record, exhausted, err := service.Complete(t.Context(), input)
	if !errors.Is(err, meteringstore.ErrReportReceiptConflict) || record.ID != 0 || exhausted {
		t.Fatalf("foreign identity accepted through affected-row count: %+v %t %v", record, exhausted, err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 30, subStatusActive)
	var count int64
	if err := h.db.Model(&model.TrafficRecord{}).Where("node_id = ? AND report_id = ?", c.NodeID, conflictID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("conflicting financial row escaped rollback", count, err)
	}
	if err := h.db.Model(&model.FlowUsage{}).Where("node_id = ? AND flow_id = ?", c.NodeID, metering.FlowUsageKey(input.CoreInstanceID, input.FlowID)).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("conflicting cumulative cursor escaped rollback", count, err)
	}
	var kept model.MeteringReportReceipt
	if err := h.db.Where("node_id = ? AND report_id = ?", c.NodeID, conflictID).First(&kept).Error; err != nil || kept.TrafficRecordID != original.ID {
		t.Fatal("conflict silently rewrote earlier identity", err)
	}
}
