package handler

import (
	"context"
	"errors"
	capabilityjobs "github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestMySQLAccountingWaitDoesNotHoldNodeStatsWriteLock(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLOrderFixture(t))
	node, _ := receiptFixture(t, h)
	// Use the same billable node; receiptFixture supplies a separate authenticated
	// node only for spool setup, not for the lock assertion.
	if err := h.db.Model(&model.Node{}).Where("id = ?", credential.NodeID).Update("node_credential", node.NodeCredential).Error; err != nil {
		t.Fatal(err)
	}
	node.ID = credential.NodeID
	lock := h.db.Begin()
	if lock.Error != nil {
		t.Fatal(lock.Error)
	}
	defer lock.Rollback()
	var subscription model.Subscription
	if err := lock.Clauses(clause.Locking{Strength: "UPDATE"}).First(&subscription, credential.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	events := accountingBenchmarkEvents(credential, 0, 1)
	events = append(events, zeroevent.Envelope{ID: "stats-lock-order", NodeID: uint64(node.ID), Type: "stats.sampled", CoreInstanceID: "benchmark-core", Sequence: 2, OccurredAt: time.Now().UTC(), Payload: []byte(`{"active_sessions":3,"bytes_up":100,"bytes_down":200}`)})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.projectZeroNodeEvents(ctx, events) }()
	var schema string
	if err := h.db.Raw("SELECT DATABASE()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		var waits int64
		err := h.db.Raw(`SELECT COUNT(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID WHERE l.OBJECT_SCHEMA=? AND l.OBJECT_NAME='subscriptions'`, schema).Scan(&waits).Error
		if err != nil {
			t.Fatal(err)
		}
		if waits > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("accounting did not reach held subscription lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	applied, err := h.services.NodeActivity.TryReceipt(context.Background(), node.ID, node.NodeCredential, time.Now().UTC())
	// Always release and join the projector, including on assertion failures.
	if commitErr := lock.Commit().Error; commitErr != nil {
		t.Fatal(commitErr)
	}
	projectionErr := <-done
	if err != nil || !applied {
		t.Fatalf("accounting waiting on subscription held node write lock: applied=%v error=%v", applied, err)
	}
	if projectionErr != nil {
		t.Fatal(projectionErr)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 30, subStatusActive)
	if err := h.db.First(&node, node.ID).Error; err != nil || node.ActiveFlows != 3 {
		t.Fatalf("stats not committed with accounting: %v", err)
	}
}

func TestMySQLNodeStatsFailureStillRollsBackAccounting(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLOrderFixture(t))
	injected := errors.New("fixture stats write failure")
	const callback = "test:stats-write-failure"
	if err := h.db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "nodes" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.db.Callback().Update().Remove(callback) })
	events := accountingBenchmarkEvents(credential, 0, 1)
	events = append(events, zeroevent.Envelope{ID: "stats-failure", NodeID: uint64(credential.NodeID), Type: "stats.sampled", Sequence: 2, OccurredAt: time.Now().UTC(), Payload: []byte(`{"active_sessions":3,"bytes_up":100,"bytes_down":200}`)})
	if err := h.projectZeroNodeEvents(context.Background(), events); !errors.Is(err, injected) {
		t.Fatalf("missing injected failure: %v", err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 0, subStatusActive)
	for _, table := range []string{"traffic_records", "flow_usages", "traffic_usage_hourly", "zero_event_node_cursors"} {
		var count int64
		if err := h.db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("rollback left %s rows=%d error=%v", table, count, err)
		}
	}
	h.db.Callback().Update().Remove(callback)
	if err := h.projectZeroNodeEvents(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 30, subStatusActive)
	if err := h.projectZeroNodeEvents(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 30, subStatusActive)
}

func TestMySQLConsumerBoundsTransactionsAndDrainsBacklog(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLOrderFixture(t))
	count := zeroEventConsumerBurstBatches*zeroEventMySQLBatchLimit + 1
	spool := &checkpointTestSpool{events: accountingBenchmarkEvents(credential, 0, count)}
	runtime := &zeroEventRuntime{spool: spool, config: zeroevent.ConsumerConfig{MaxBatch: 2000}}
	for spool.position < count {
		err := h.consumeZeroEventCycle(context.Background(), runtime)
		if err != nil && !errors.Is(err, capabilityjobs.ErrContinue) {
			t.Fatal(err)
		}
		if len(spool.limits) > zeroEventConsumerBurstBatches+2 {
			t.Fatal("consumer stopped advancing checkpoints")
		}
	}
	for _, limit := range spool.limits {
		if limit != 128 {
			t.Fatalf("unbounded MySQL transaction: %d", limit)
		}
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, int64(count*30), subStatusActive)
}
