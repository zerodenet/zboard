package handler

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestSQLiteDiagnosticRetirementPreservesAccountingReplayAndProjection(t *testing.T) {
	assertDiagnosticRetirementAccounting(t, newOrderFixture(t))
}
func TestMySQLDiagnosticRetirementPreservesAccountingReplayAndProjection(t *testing.T) {
	assertDiagnosticRetirementAccounting(t, newMySQLBusinessOrderFixture(t))
}

func assertDiagnosticRetirementAccounting(t *testing.T, f orderFixture) {
	t.Helper()
	h, c := accountingFixtureFromOrder(t, f)
	if err := h.db.Model(&model.Node{}).Where("id = ?", c.NodeID).Update("traffic_secret", "diagnostic-retention-test-key").Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id = ?", c.ProtocolEndpointID).Update("multiplier_milli", 1500).Error; err != nil {
		t.Fatal(err)
	}
	input := metering.AuthenticatedNodeReport{NodeID: c.NodeID, UserID: c.UserID, ProtocolEndpointID: c.ProtocolEndpointID, ExpectedCredential: "diagnostic-retention-test-key", Timestamp: time.Now().UTC(), Nonce: "diagnostic-retention-first", ReportID: "diagnostic-retention-first", DownloadBytes: 3, Meta: strings.Repeat("诊断", 1000) + "\x00tail"}
	service := h.services.NodeReports()
	first, err := service.Record(t.Context(), input)
	if err != nil || first.Duplicate || first.Record.UsedBytes != 5 || first.Record.RawBytes != 3 {
		t.Fatal("real legacy accounting", err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 5, subStatusActive)
	var before model.TrafficRecord
	if err := h.db.First(&before, first.Record.ID).Error; err != nil {
		t.Fatal(err)
	}
	var pendingBefore int64
	if err := h.db.Model(&model.MeteringUsageProjectionPending{}).Count(&pendingBefore).Error; err != nil || pendingBefore != 1 {
		t.Fatal("reliable intent missing", pendingBefore, err)
	}
	// Failing the durable retirement marker must roll back the Meta replacement.
	trigger := `CREATE TRIGGER reject_diagnostic_marker BEFORE UPDATE OF retired_at ON traffic_diagnostic_materials BEGIN SELECT RAISE(ABORT,'marker unavailable'); END`
	if !datastore.IsSQLite(h.db) {
		trigger = `CREATE TRIGGER reject_diagnostic_marker BEFORE UPDATE ON traffic_diagnostic_materials FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='marker unavailable'`
	}
	if err := h.db.Exec(trigger).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(8 * 24 * time.Hour)
	worker := meteringstore.DiagnosticMaterialRetention{DB: h.db, Now: func() time.Time { return now }}
	if _, err := worker.Retire(t.Context(), 32, meteringstore.DiagnosticMaterialReadBudget); err == nil {
		t.Fatal("failed marker acknowledged")
	}
	var after model.TrafficRecord
	if err := h.db.First(&after, first.Record.ID).Error; err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("failed retirement changed the accounting source", err)
	}
	if err := h.db.Exec("DROP TRIGGER reject_diagnostic_marker").Error; err != nil {
		t.Fatal(err)
	}
	if out, err := worker.Retire(t.Context(), 32, meteringstore.DiagnosticMaterialReadBudget); err != nil || out.Retired != 1 {
		t.Fatal("retirement retry", out, err)
	}
	if err := h.db.First(&after, first.Record.ID).Error; err != nil {
		t.Fatal(err)
	}
	var receipt struct{ Type string }
	if err := json.Unmarshal([]byte(after.Meta), &receipt); err != nil || receipt.Type != "diagnostic_meta_retired" {
		t.Fatal("retired body not explained", err)
	}
	before.Meta = after.Meta
	if !reflect.DeepEqual(before, after) {
		t.Fatal("non-diagnostic financial/source fields changed")
	}
	for range 2 {
		replay, err := service.Record(t.Context(), input)
		if err != nil || !replay.Duplicate || replay.Record.ID != first.Record.ID || replay.Record.Meta != after.Meta {
			t.Fatal("late replay recreated material/settled again", err)
		}
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 5, subStatusActive)
	var pendingAfter int64
	if err := h.db.Model(&model.MeteringUsageProjectionPending{}).Count(&pendingAfter).Error; err != nil || pendingAfter != pendingBefore {
		t.Fatal("retirement/replay changed pending projections", pendingAfter, err)
	}
	if _, err := h.services.UsageProjections.Drain(t.Context(), 200); err != nil {
		t.Fatal("projection after diagnostic retirement", err)
	}
	var daily model.ProtocolEndpointUsageDaily
	if err := h.db.Where("protocol_endpoint_id = ?", c.ProtocolEndpointID).First(&daily).Error; err != nil || daily.UsedBytes != 5 {
		t.Fatal("narrow projection lost original rounded accounting", err)
	}
	// The classification receipt is part of admission, not a disposable queue:
	// failure rejects the new report completely and leaves its source replayable.
	createTrigger := `CREATE TRIGGER reject_diagnostic_admission BEFORE INSERT ON traffic_diagnostic_materials BEGIN SELECT RAISE(ABORT,'receipt unavailable'); END`
	if !datastore.IsSQLite(h.db) {
		createTrigger = `CREATE TRIGGER reject_diagnostic_admission BEFORE INSERT ON traffic_diagnostic_materials FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='receipt unavailable'`
	}
	if err := h.db.Exec(createTrigger).Error; err != nil {
		t.Fatal(err)
	}
	input.Nonce = "diagnostic-retention-second"
	input.ReportID = input.Nonce
	if _, err := service.Record(t.Context(), input); err == nil {
		t.Fatal("missing provenance was acknowledged")
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 5, subStatusActive)
	if err := h.db.Exec("DROP TRIGGER reject_diagnostic_admission").Error; err != nil {
		t.Fatal(err)
	}
	if retry, err := service.Record(t.Context(), input); err != nil || retry.Duplicate {
		t.Fatal("rejected admission cannot be explicitly retried", err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 10, subStatusActive)
	var n int64
	if err := h.db.Model(&model.TrafficDiagnosticMaterial{}).Count(&n).Error; err != nil || n != 2 {
		t.Fatal("duplicate/orphan provenance", n, err)
	}
}
