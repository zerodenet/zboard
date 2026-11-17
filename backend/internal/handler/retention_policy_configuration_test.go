package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestRetentionConfigurationPreservesOverridesAndPrunesExpiredLogs(t *testing.T) {
	assertRetentionConfiguration(t, newOrderFixture(t))
}
func TestMySQLRetentionConfigurationPreservesOverridesAndPrunesExpiredLogs(t *testing.T) {
	assertRetentionConfiguration(t, newMySQLBusinessOrderFixture(t))
}
func assertRetentionConfiguration(t *testing.T, f orderFixture) {
	t.Helper()
	h := f.h
	prepareTestMaintenanceSource(t, h)
	if e := h.ReconcileHistoryRetentionDefaults(); e != nil {
		t.Fatal(e)
	}
	set := func(key, value string) {
		t.Helper()
		_, e := h.services.SettingUpdate(h.credentialCipher).Update(t.Context(), 1, platform.SettingUpdateInput{Key: key, Value: json.RawMessage(value)})
		if e != nil {
			t.Fatal(key, e)
		}
	}
	set(observability.AuditLogRetentionKey, "14")
	if e := h.ReconcileHistoryRetentionDefaults(); e != nil {
		t.Fatal(e)
	}
	view, e := h.services.Settings.View(t.Context(), observability.AuditLogRetentionKey)
	if e != nil || view.DefaultValue == nil || *view.DefaultValue != 30 || fmt.Sprint(view.Value) != "14" {
		t.Fatal("operator override/default metadata", view, e)
	}
	if _, e := h.services.SettingUpdate(h.credentialCipher).Update(t.Context(), 1, platform.SettingUpdateInput{Key: observability.AuditLogRetentionKey, Value: json.RawMessage(`-1`)}); e == nil {
		t.Fatal("negative lifetime accepted")
	}
	now := time.Now().UTC()
	actorID := uint(1)
	old := model.AuditLog{UserID: &actorID, Actor: "retention-test", Action: "test", Target: "expired", CreatedAt: now.Add(-15 * 24 * time.Hour)}
	young := old
	young.Target = "recent"
	young.CreatedAt = now
	if e := h.db.Create(&old).Error; e != nil {
		t.Fatal(e)
	}
	if e := h.db.Create(&young).Error; e != nil {
		t.Fatal(e)
	}
	set(observability.CleanupIntervalKey, "0")
	due, e := h.services.HistoryRetention.CleanupDue(t.Context(), now)
	if e != nil || due {
		t.Fatal("disabled cleanup dispatched", due, e)
	}
	set(observability.CleanupIntervalKey, "1")
	due, e = h.services.HistoryRetention.CleanupDue(t.Context(), now)
	if e != nil || !due {
		t.Fatal("enabled policy not applied live", due, e)
	}
	if _, e := h.services.HistoryRetention.RunTarget(t.Context(), now, observability.AuditLogs); e != nil {
		t.Fatal(e)
	}
	for id, want := range map[uint]int64{old.ID: 0, young.ID: 1} {
		var count int64
		if e := h.db.Model(&model.AuditLog{}).Where("id=?", id).Count(&count).Error; e != nil || count != want {
			t.Fatal("retention selected wrong rows", id, count, e)
		}
	}
}

func TestMySQLCompletedAuxiliaryCleanupUsesConfiguredLifetime(t *testing.T) {
	f := newMySQLBusinessOrderFixture(t)
	h := f.h
	prepareTestMaintenanceSource(t, h)
	if e := h.ReconcileHistoryRetentionDefaults(); e != nil {
		t.Fatal(e)
	}
	endpoint := attachOrderPublishEndpoint(t, f)
	var node model.Node
	if e := h.db.First(&node, endpoint.NodeID).Error; e != nil {
		t.Fatal(e)
	}
	if e := h.db.Model(&model.NodeObservationMaterialBudget{}).Where("id=1").Update("retained_bytes", observationstore.MaterialCapacityBytes).Error; e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	source := network.ObservationSource{Observation: network.NodeObservation{NodeID: node.ID, Latest: network.EventPosition{ID: "retention-policy-test", AssetTag: node.AssetTag, CoreInstanceID: "retention-core", Sequence: 1, OccurredAt: now, ReceivedAt: now}}, Kind: "flow.updated"}
	var id string
	if e := h.db.Transaction(func(tx *gorm.DB) error { var e error; id, e = observationstore.Enqueue(tx, source); return e }); e != nil {
		t.Fatal(e)
	}
	// Simulate an old completed legacy auxiliary record. Its source proof is no
	// longer a condition for deleting already-retired, unreferenced metadata.
	if e := h.db.Model(&model.NodeObservationIntent{}).Where("id=?", id).UpdateColumns(map[string]any{"created_at": now.Add(-48 * time.Hour), "material_retired_at": now.Add(-48 * time.Hour)}).Error; e != nil {
		t.Fatal(e)
	}
	if e := h.db.Model(&model.NodeObservationRetirement{}).Where("intent_id=?", id).UpdateColumn("updated_at", now.Add(-48*time.Hour)).Error; e != nil {
		t.Fatal(e)
	}
	store := observationstore.MaterialRetention{DB: h.db}
	set := func(value string) {
		t.Helper()
		if _, e := h.services.SettingUpdate(h.credentialCipher).Update(t.Context(), 1, platform.SettingUpdateInput{Key: observability.ObservationReceiptDaysKey, Value: json.RawMessage(value)}); e != nil {
			t.Fatal(e)
		}
	}
	set("0")
	if _, e := store.Retire(context.Background(), 32, observationstore.MaxDrainBytes); e != nil {
		t.Fatal(e)
	}
	var count int64
	h.db.Model(&model.NodeObservationIntent{}).Where("id=?", id).Count(&count)
	if count != 1 {
		t.Fatal("disabled policy deleted metadata")
	}
	set("1")
	if _, e := store.Retire(t.Context(), 32, observationstore.MaxDrainBytes); e != nil {
		t.Fatal(e)
	}
	h.db.Model(&model.NodeObservationIntent{}).Where("id=?", id).Count(&count)
	if count != 0 {
		t.Fatal("expired auxiliary metadata not deleted")
	}
}
