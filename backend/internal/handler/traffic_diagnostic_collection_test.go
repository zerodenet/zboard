package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
)

// Fixture preparation is explicit: collection is no longer enabled by merely
// providing a Meta string. Production admission uses the ordinary admin command.
func enableDiagnosticCollectionFixture(t testing.TB, h *handlers) {
	t.Helper()
	now := time.Now().UTC()
	value, err := json.Marshal(metering.DiagnosticCollectionPolicy{Until: now.Add(time.Hour), Reason: "explicit diagnostic test fixture"})
	if err != nil {
		t.Fatal(err)
	}
	var row model.SystemConfig
	if err := h.db.Where("config_key = ?", metering.DiagnosticCollectionKey).Assign(model.SystemConfig{Value: string(value), ValueType: "json", IsPublic: false, IsSecret: false}).FirstOrCreate(&row, model.SystemConfig{ConfigKey: metering.DiagnosticCollectionKey}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteDiagnosticCollectionIsOptInBoundedAndAudited(t *testing.T) {
	assertDiagnosticCollection(t, newOrderFixture(t))
}
func TestMySQLDiagnosticCollectionIsOptInBoundedAndAudited(t *testing.T) {
	assertDiagnosticCollection(t, newMySQLBusinessOrderFixture(t))
}

func assertDiagnosticCollection(t *testing.T, f orderFixture) {
	t.Helper()
	h, c := accountingFixtureFromOrder(t, f)
	prepareTestMaintenanceSource(t, h)
	if err := h.ReconcileSystemConfigDefaults(); err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&model.Node{}).Where("id = ?", c.NodeID).Update("traffic_secret", "diagnostic-collection-key").Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id = ?", c.ProtocolEndpointID).Update("multiplier_milli", 1500).Error; err != nil {
		t.Fatal(err)
	}
	var setting model.SystemConfig
	if err := h.db.Where("config_key = ?", metering.DiagnosticCollectionKey).First(&setting).Error; err != nil || setting.Value != "null" || setting.IsPublic || setting.IsSecret {
		t.Fatal("default collection is not private/off", setting, err)
	}
	service := h.services.NodeReports()
	sequence := 0
	var charged int64
	submit := func(meta string) (metering.AuthenticatedNodeReport, metering.NodeReportResult) {
		t.Helper()
		sequence++
		id := fmt.Sprintf("capture-%d", sequence)
		in := metering.AuthenticatedNodeReport{NodeID: c.NodeID, UserID: c.UserID, ProtocolEndpointID: c.ProtocolEndpointID, ExpectedCredential: "diagnostic-collection-key", Timestamp: time.Now().UTC(), Nonce: id, ReportID: id, DownloadBytes: 3, Meta: meta}
		out, err := service.Record(t.Context(), in)
		if err != nil || out.Duplicate || out.Record.RawBytes != 3 || out.Record.UsedBytes != 5 {
			t.Fatal("optional capture changed financial admission", err)
		}
		charged += 5
		assertAccountingTotal(t, h, c.SubscriptionID, charged, subStatusActive)
		return in, out
	}
	assertOmitted := func(out metering.NodeReportResult, reason string) {
		t.Helper()
		var receipt struct{ Type, Reason string }
		if err := json.Unmarshal([]byte(out.Record.Meta), &receipt); err != nil || receipt.Type != "diagnostic_meta_omitted" || receipt.Reason != reason || len(out.Record.Meta) > 512 {
			t.Fatal("capture omission not explicit", receipt, err)
		}
		var headers int64
		if err := h.db.Model(&model.TrafficDiagnosticMaterial{}).Where("traffic_record_id = ?", out.Record.ID).Count(&headers).Error; err != nil || headers != 0 {
			t.Fatal("uncaptured body classified", headers, err)
		}
	}
	disabledInput, disabled := submit("must-not-persist-by-default")
	assertOmitted(disabled, "collection_disabled")
	// A successful financial fact with diagnostics disabled is not replayed as
	// a fresh capture after an administrator later enables the window.
	revision := setting.Revision
	policy := metering.DiagnosticCollectionPolicy{Until: time.Now().UTC().Add(time.Hour), Reason: "support case capture"}
	value, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	settings := h.services.SettingUpdate(h.credentialCipher)
	in := platform.SettingUpdateInput{Key: metering.DiagnosticCollectionKey, Value: value, ExpectedRevision: &revision}
	view, err := settings.Update(t.Context(), 1, in)
	if err != nil || view.Revision != revision+1 {
		t.Fatal("admin capture command", view, err)
	}
	if _, err := settings.Update(t.Context(), 1, in); !errors.Is(err, platform.ErrSettingRevision) {
		t.Fatal("stale capture policy accepted", err)
	}
	var audit model.AuditLog
	if err := h.db.Where("action = ? AND target = ?", "system.config.update", "system_config:"+metering.DiagnosticCollectionKey).Last(&audit).Error; err != nil || audit.UserID == nil || *audit.UserID != 1 || !strings.Contains(audit.Detail, policy.Reason) {
		t.Fatal("capture reason/actor not audited", err)
	}
	if err := h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	in.ExpectedRevision = nil
	if _, err := settings.Update(t.Context(), 1, in); !errors.Is(err, platform.ErrSettingsPermission) {
		t.Fatal("revoked capture administrator accepted", err)
	}
	if err := h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	if out, err := service.Record(t.Context(), disabledInput); err != nil || !out.Duplicate || out.Record.Meta != disabled.Record.Meta {
		t.Fatal("enabled window recaptured previous financial identity", err)
	}
	firstInput, first := submit("capture-small")
	if first.Record.Meta != firstInput.Meta {
		t.Fatal("explicit capture omitted")
	}
	for i := 1; i < metering.DiagnosticCollectionSlots; i++ {
		request, out := submit(strings.Repeat("m", 65535))
		if out.Record.Meta != request.Meta {
			t.Fatal("capacity reached before configured bound", i)
		}
	}
	capInput, capResult := submit("full-capacity-continues-accounting")
	assertOmitted(capResult, "collection_capacity")
	for range 2 {
		out, err := service.Record(t.Context(), capInput)
		if err != nil || !out.Duplicate || out.Record.ID != capResult.Record.ID || out.Record.Meta != capResult.Record.Meta {
			t.Fatal("full capacity replay changed receipt", err)
		}
	}
	var pending int64
	if err := h.db.Model(&model.TrafficDiagnosticMaterial{}).Where("retired_at IS NULL").Count(&pending).Error; err != nil || pending != 128 {
		t.Fatal("capture capacity exceeded", pending, err)
	}
	// Retirement frees one body slot, not a financial or nonce identity.
	worker := meteringstore.DiagnosticMaterialRetention{DB: h.db, Now: func() time.Time { return time.Now().UTC().Add(8 * 24 * time.Hour) }}
	if out, err := worker.Retire(t.Context(), 1, meteringstore.DiagnosticMaterialReadBudget); err != nil || out.Retired != 1 {
		t.Fatal("capacity retirement", out, err)
	}
	if out, err := service.Record(t.Context(), firstInput); err != nil || !out.Duplicate || out.Record.Meta == firstInput.Meta {
		t.Fatal("retired replay recreated body", err)
	}
	_, replacement := submit(strings.Repeat("r", 65535))
	if len(replacement.Record.Meta) != 65535 {
		t.Fatal("retired capacity did not reopen")
	}
	if err := h.db.Model(&model.TrafficDiagnosticMaterial{}).Where("retired_at IS NULL").Count(&pending).Error; err != nil || pending != 128 {
		t.Fatal("refilled cap exceeded", pending, err)
	}
	var bodyBytes int64
	if err := h.db.Table("traffic_diagnostic_materials AS d").Joins("JOIN traffic_records AS t ON t.id = d.traffic_record_id").Where("d.retired_at IS NULL").Select("COALESCE(SUM(octet_length(t.meta)),0)").Scan(&bodyBytes).Error; err != nil || bodyBytes != int64(128*65535) {
		t.Fatal("actual pending body bytes exceed/differ from capacity", bodyBytes, err)
	}
	if _, err := settings.Update(t.Context(), 1, platform.SettingUpdateInput{Key: metering.DiagnosticCollectionKey, Value: json.RawMessage("null")}); err != nil {
		t.Fatal(err)
	}
	_, off := submit("explicitly-disabled")
	assertOmitted(off, "collection_disabled")
	if err := h.db.Model(&model.TrafficDiagnosticMaterial{}).Where("retired_at IS NULL").Count(&pending).Error; err != nil || pending != 128 {
		t.Fatal("disable erased retained sources", pending, err)
	}
	bad, _ := json.Marshal(metering.DiagnosticCollectionPolicy{Until: time.Now().UTC().Add(25 * time.Hour), Reason: "too long"})
	var invalid *platform.SettingValidation
	if _, err := settings.Update(t.Context(), 1, platform.SettingUpdateInput{Key: metering.DiagnosticCollectionKey, Value: bad}); !errors.As(err, &invalid) {
		t.Fatal("unbounded window accepted", err)
	}
	if err := h.db.Model(&model.SystemConfig{}).Where("config_key = ?", metering.DiagnosticCollectionKey).UpdateColumn("value", `{"unexpected":true}`).Error; err != nil {
		t.Fatal(err)
	}
	_, corrupt := submit("invalid-policy-still-financial")
	assertOmitted(corrupt, "collection_policy_invalid")
	if _, err := h.services.UsageProjections.Drain(t.Context(), 200); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, charged, subStatusActive)
}
