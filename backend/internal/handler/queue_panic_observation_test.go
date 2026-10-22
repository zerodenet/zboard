package handler

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

const queuePrivatePanic = "queue-private-panic-material"

func recoveredQueuePanic(run func()) (value any, returned bool) {
	defer func() { value = recover() }()
	run()
	return nil, true
}

func assertQueuePanicObservation(t *testing.T, h *handlers, id string) {
	t.Helper()
	for _, row := range h.backgroundJobs().observations.Snapshot() {
		if row.ID != id {
			continue
		}
		if row.Runs != 1 || row.Running != 0 || row.Failures != 1 || row.LastResult != "failed" || row.LastError != periodicSafePanic {
			t.Errorf("queue panic observation remained unfinished: %+v", row)
		}
		return
	}
	t.Error("queue panic observation missing")
}

func TestSQLitePublicationPanicRetainsLeaseAndFinishesObservation(t *testing.T) {
	h, _ := newPublishFixture(t)
	assertPublicationPanicRetainsLeaseAndFinishesObservation(t, h)
}

func TestMySQLPublicationPanicRetainsLeaseAndFinishesObservation(t *testing.T) {
	h, _ := newMySQLPublishHandlers(t)
	if err := h.db.Create(&model.Node{ID: 1, Name: "panic-publish", Config: "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	assertPublicationPanicRetainsLeaseAndFinishesObservation(t, h)
}

func assertPublicationPanicRetainsLeaseAndFinishesObservation(t *testing.T, h *handlers) {
	t.Helper()
	mustEnqueuePublish(t, h, 11)
	item := mustClaimPublish(t, h, time.Now().UTC().Add(time.Second))
	var before model.NodeConfigPublish
	if err := h.db.First(&before, item.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	calls := 0
	value, returned := recoveredQueuePanic(func() {
		_ = h.executeNodePublish(t.Context(), item, func(context.Context) error {
			calls++
			panic(queuePrivatePanic)
		})
	})
	if returned || value != queuePrivatePanic || calls != 1 {
		t.Fatal("publication panic was swallowed, changed or repeated")
	}
	var after model.NodeConfigPublish
	if err := h.db.First(&after, item.NodeID).Error; err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("panic must retain the exact pending publication and lease for recovery", err)
	}
	assertNoPublishClaim(t, h, time.Now().UTC())
	assertQueuePanicObservation(t, h, "node_publish")
}

type panicCheckpointSpool struct {
	*checkpointTestSpool
	panicCommit bool
}

func (s *panicCheckpointSpool) Commit(ctx context.Context, next zeroevent.Checkpoint) error {
	if s.panicCommit {
		panic(queuePrivatePanic)
	}
	return s.checkpointTestSpool.Commit(ctx, next)
}

func TestSQLiteCheckpointPanicReplaysAccountingAndFinishesObservation(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	assertCheckpointPanicReplaysAccountingAndFinishesObservation(t, h, credential)
}

func TestMySQLCheckpointPanicReplaysAccountingAndFinishesObservation(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	assertCheckpointPanicReplaysAccountingAndFinishesObservation(t, h, credential)
}

func assertCheckpointPanicReplaysAccountingAndFinishesObservation(t *testing.T, h *handlers, credential model.ProtocolCredential) {
	t.Helper()
	// This is the existing checkpoint fault probe, not an ingestion ACK or file
	// durability test. Accounting, cumulative cursors and completion use real SQL.
	s := &panicCheckpointSpool{checkpointTestSpool: &checkpointTestSpool{events: accountingBenchmarkEvents(credential, 0, 1)}, panicCommit: true}
	value, returned := recoveredQueuePanic(func() { _, _ = h.consumeZeroEventBatch(t.Context(), s, 32) })
	if returned || value != queuePrivatePanic || s.position != 0 {
		t.Fatal("checkpoint panic was swallowed, changed or advanced the cursor")
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 30, subStatusActive)
	assertQueuePanicObservation(t, h, "event_consumer")
	// Continue even when the old observer assertion reports an error: the
	// regression control must also prove no duplicate financial/usage effect.
	s.panicCommit = false
	if count, err := h.consumeZeroEventBatch(t.Context(), s, 32); err != nil || count != 1 || s.position != 1 {
		t.Fatal("retained checkpoint could not be replayed", count, s.position, err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 30, subStatusActive)
	if count, err := h.consumeZeroEventBatch(t.Context(), s, 32); err != nil || count != 0 {
		t.Fatal("replayed checkpoint advanced twice", count, err)
	}
	for _, row := range h.backgroundJobs().observations.Snapshot() {
		if row.ID == "event_consumer" {
			if row.Runs != 2 || row.Running != 0 || row.Failures != 1 || row.LastResult != "succeeded" || row.LastError != "" {
				t.Errorf("successful replay left a stuck observation or lost the earlier failure: %+v", row)
			}
			return
		}
	}
	t.Error("replayed consumer observation missing")
}
