package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"gorm.io/gorm"
)

func TestObservationFailureDoesNotRollBackSettlement(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	assertObservationSettlementHandoff(t, h, credential)
}
func assertObservationSettlementHandoff(t *testing.T, h *handlers, credential model.ProtocolCredential) {
	t.Helper()
	ctx := context.Background()
	var node model.Node
	if err := h.db.First(&node, credential.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	events := accountingBenchmarkEvents(credential, 0, 1)
	events[0].AssetTag = node.AssetTag
	at := time.Now().UTC()
	events = append(events, zeroevent.Envelope{ID: "handoff-stats", NodeID: uint64(node.ID), AssetTag: node.AssetTag, Type: "stats.sampled", CoreInstanceID: "benchmark-core", Sequence: 2, OccurredAt: at, ReceivedAt: at, Payload: []byte(`{"active_sessions":3,"bytes_up":100,"bytes_down":200}`)})
	injected := errors.New("auxiliary node persistence unavailable")
	const hook = "test:observation_nodes"
	if err := h.db.Callback().Update().Before("gorm:update").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table == "nodes" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.db.Callback().Update().Remove(hook) })
	if err := h.projectZeroNodeEvents(ctx, events); err != nil {
		t.Fatal("auxiliary failure blocked financial authority", err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 30, subStatusActive)
	var sourceCount int64
	if err := h.db.Model(&model.NodeObservationIntent{}).Count(&sourceCount).Error; err != nil || sourceCount != 2 {
		t.Fatal("settlement lacks complete durable sources", sourceCount, err)
	}
	if _, err := h.services.NodeObservations.Drain(ctx, 32, 8*1024*1024); !errors.Is(err, injected) {
		t.Fatal("projection failure disappeared instead of durable retry", err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 30, subStatusActive)
	if err := h.db.First(&node, node.ID).Error; err != nil || node.ActiveFlows != 0 {
		t.Fatal("failed projection committed partial counters", err)
	}
	_ = h.db.Callback().Update().Remove(hook)
	if err := h.db.Model(&model.NodeObservationIntent{}).Where("status = ?", "pending").Update("available_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := h.services.NodeObservations.Drain(ctx, 32, 8*1024*1024); err != nil {
		t.Fatal(err)
	}
	if err := h.db.First(&node, node.ID).Error; err != nil || node.ActiveFlows != 3 {
		t.Fatal("retry did not apply retained Stats", err)
	}
	if err := h.projectZeroNodeEvents(ctx, events); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 30, subStatusActive)
	var after int64
	if err := h.db.Model(&model.NodeObservationIntent{}).Count(&after).Error; err != nil || after != sourceCount {
		t.Fatal("spool replay changed source identity", err)
	}
}
func TestObservationAppendFailureRollsBackSettlementAndSpoolAck(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	events := accountingBenchmarkEvents(credential, 0, 1)
	spool := &checkpointTestSpool{events: events}
	injected := errors.New("durable source parts unavailable")
	const hook = "test:observation_parts"
	if err := h.db.Callback().Create().Before("gorm:create").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table == "node_observation_parts" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, err := h.consumeZeroEventBatch(context.Background(), spool, 2000)
	_ = h.db.Callback().Create().Remove(hook)
	if !errors.Is(err, injected) {
		t.Fatal("source append failure swallowed", err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 0, subStatusActive)
	if spool.position != 0 {
		t.Fatal("failed handoff ACKed original financial source")
	}
	var count int64
	if err := h.db.Model(&model.NodeObservationIntent{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("failed authority transaction left intent", err)
	}
	if _, err := h.consumeZeroEventBatch(context.Background(), spool, 2000); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 30, subStatusActive)
	if spool.position != 1 {
		t.Fatal("successful replay did not ACK")
	}
}
