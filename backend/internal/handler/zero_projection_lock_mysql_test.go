package handler

import (
	"context"
	"errors"
	capabilityjobs "github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	capabilitynetwork "github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"gorm.io/gorm/clause"
)

func TestMySQLAccountingWaitDoesNotHoldNodeStatsWriteLock(t *testing.T) {
	assertAccountingWaitNodeLock(t, false)
}

func TestMySQLAccountingWaitRechecksConcurrentNodeDeletion(t *testing.T) {
	assertAccountingWaitNodeLock(t, true)
}

func assertAccountingWaitNodeLock(t *testing.T, deleting bool) {
	t.Helper()
	h, credential := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
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
	var asset model.Node
	if err := h.db.First(&asset, credential.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	for i := range events {
		events[i].AssetTag = asset.AssetTag
	}
	events = append(events, zeroevent.Envelope{ID: "stats-lock-order", NodeID: uint64(node.ID), AssetTag: asset.AssetTag, ReceivedAt: time.Now().UTC(), Type: "stats.sampled", CoreInstanceID: "benchmark-core", Sequence: 2, OccurredAt: time.Now().UTC(), Payload: []byte(`{"active_sessions":3,"bytes_up":100,"bytes_down":200}`)})
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
	// The preliminary lifecycle snapshot was already read before this wait.
	// A current parent fence must still reject closure committed during the wait.
	if deleting {
		if e := h.db.Model(&model.Node{}).Where("id = ?", node.ID).Update("lifecycle_status", "deleting").Error; e != nil {
			t.Fatal(e)
		}
	}
	// Always release and join the projector, including on assertion failures.
	if commitErr := lock.Commit().Error; commitErr != nil {
		t.Fatal(commitErr)
	}
	projectionErr := <-done
	if err != nil || !applied {
		t.Fatalf("accounting waiting on subscription held node write lock: applied=%v error=%v", applied, err)
	}
	if deleting {
		if !errors.Is(projectionErr, capabilitynetwork.ErrNodeAdministrationDeleting) {
			t.Fatal("stale lifecycle snapshot admitted accounting", projectionErr)
		}
		assertAccountingTotal(t, h, credential.SubscriptionID, 0, subStatusActive)
		for _, table := range []any{&model.FlowUsage{}, &model.TrafficRecord{}} {
			var count int64
			if e := h.db.Model(table).Where("node_id = ?", node.ID).Count(&count).Error; e != nil || count != 0 {
				t.Fatal("closed node wrote accounting facts", count, e)
			}
		}
		// The unchanged spool is replayable: the next snapshot omits closed flows.
		if e := h.projectZeroNodeEvents(context.Background(), events); e != nil {
			t.Fatal("closed source prevented replay", e)
		}
		assertAccountingTotal(t, h, credential.SubscriptionID, 0, subStatusActive)
		return
	}
	if projectionErr != nil {
		t.Fatal(projectionErr)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 30, subStatusActive)
	if _, err := h.services.NodeObservations.Drain(context.Background(), 32, 8*1024*1024); err != nil {
		t.Fatal(err)
	}
	if err := h.db.First(&node, node.ID).Error; err != nil || node.ActiveFlows != 3 {
		t.Fatalf("committed observation intent did not apply after accounting: %v", err)
	}
}

func TestMySQLNodeStatsFailureRetainsSettlementAndRetriesProjection(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	assertObservationSettlementHandoff(t, h, credential)
}

func TestMySQLConsumerBoundsTransactionsAndDrainsBacklog(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
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
		if limit != 2000 {
			t.Fatalf("unbounded MySQL read window: %d", limit)
		}
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, int64(count*30), subStatusActive)
}
