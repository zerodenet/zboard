package handler

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestObservationMaterialRetirementKeepsBillingAndCheckpointReplay(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	assertRetiredMaterialBillingReplay(t, h, c)
}
func TestMySQLObservationMaterialRetirementKeepsBillingAndCheckpointReplay(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	assertRetiredMaterialBillingReplay(t, h, c)
}
func assertRetiredMaterialBillingReplay(t *testing.T, h *handlers, credential model.ProtocolCredential) {
	t.Helper()
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id = ?", credential.ProtocolEndpointID).Update("multiplier_milli", 1500).Error; err != nil {
		t.Fatal(err)
	}
	var node model.Node
	if err := h.db.First(&node, credential.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	events := repeatedFlowEvents(credential, 3)
	// Capture the original admission label once. Replays below use these exact
	// envelopes; no current-label lookup or receiver-time substitution occurs.
	for i := range events {
		events[i].AssetTag = node.AssetTag
	}
	if err := h.projectZeroNodeEvents(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 15, subStatusActive)
	if n, err := (observationstore.Worker{DB: h.db}).Drain(t.Context(), 32, observationstore.MaxDrainBytes); err != nil || n != 1 {
		t.Fatal("actual auxiliary apply", n, err)
	}
	var before model.NodeObservationState
	if err := h.db.First(&before, "node_id = ? AND asset_tag = ?", node.ID, node.AssetTag).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(8 * 24 * time.Hour)
	if out, err := (observationstore.MaterialRetention{DB: h.db, Now: func() time.Time { return now }}).Retire(t.Context(), 32, observationstore.MaxDrainBytes); err != nil || out.Retired != 0 {
		t.Fatal("actual financial-source material retirement", out, err)
	}
	spool := &checkpointTestSpool{events: events, commitErr: errors.New("retired replay checkpoint unavailable")}
	if _, err := h.consumeZeroEventBatch(t.Context(), spool, 2000); err == nil || spool.position != 0 {
		t.Fatal("failed checkpoint reported progress", spool.position, err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 15, subStatusActive)
	spool.commitErr = nil
	if count, err := h.consumeZeroEventBatch(t.Context(), spool, 2000); err != nil || count != 3 || spool.position != 3 {
		t.Fatal("bounded retired replay failed", count, spool.position, err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 15, subStatusActive)
	var rows []model.TrafficRecord
	if err := h.db.Find(&rows).Error; err != nil || len(rows) != 1 || rows[0].RawBytes != 9 || rows[0].UsedBytes != 15 || rows[0].ProtocolMultiplierMilli != 1500 {
		t.Fatal("retirement/replay changed rounded billing", rows, err)
	}
	var after model.NodeObservationState
	if err := h.db.First(&after, "node_id = ? AND asset_tag = ?", node.ID, node.AssetTag).Error; err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("checkpoint replay changed observation source", after, before, err)
	}
	for _, table := range []string{"node_observation_intents", "node_observation_parts", "node_observation_retirements"} {
		var count int64
		if e := h.db.Table(table).Count(&count).Error; e != nil || count != 0 {
			t.Fatal("per-event source growth after checkpoint replay", table, count, e)
		}
	}
	var count int64
	h.db.Model(&model.NodeObservationMailbox{}).Count(&count)
	if count != 1 {
		t.Fatal("mailbox count", count)
	}
}
