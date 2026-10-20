package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
)

func TestMySQLObservationRetentionPointLocksAllowFreshReceiveAndSettlement(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLOrderFixture(t))
	now := time.Now().UTC().Truncate(time.Millisecond)
	old := metering.FlowStart{NodeID: credential.NodeID, EventID: "old-diagnostic", CoreInstanceID: "retention-lock", PrincipalKey: credential.PrincipalKey, OccurredAt: now, ReceivedAt: now.Add(-16 * 24 * time.Hour)}
	if err := h.services.FairUseFlowCollection.Record(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	locked, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	releaseOnce := func() { once.Do(func() { close(release) }) }
	defer releaseOnce()
	const callback = "test:pause_old_observation_delete"
	if err := h.db.Callback().Delete().Before("gorm:delete").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != (meteringstore.FlowStartRecord{}).TableName() {
			return
		}
		close(locked) // the preceding exact-PK SELECT FOR UPDATE already completed
		select {
		case <-release:
		case <-ctx.Done():
			tx.AddError(ctx.Err())
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer h.db.Callback().Delete().Remove(callback)
	type completion struct {
		result metering.ObservationRetentionResult
		err    error
	}
	done := make(chan completion, 1)
	go func() {
		result, err := h.services.FairUseObservationRetention.Run(ctx, now)
		done <- completion{result, err}
	}()
	defer func() {
		releaseOnce()
		select {
		case <-done:
		case <-ctx.Done():
		}
	}()
	select {
	case <-locked:
	case <-ctx.Done():
		t.Fatal("maintenance did not reach locked old primary key", ctx.Err())
	}
	// Both operations must commit while old-row maintenance remains paused.
	// A temporal next-key lock under RR would block the fresh diagnostic insert.
	fresh := old
	fresh.EventID, fresh.ReceivedAt = "fresh-diagnostic", now
	if err := h.services.FairUseFlowCollection.Record(ctx, fresh); err != nil {
		t.Fatal("fresh reception waited behind old diagnostic retention", err)
	}
	if err := h.projectZeroNodeEvents(ctx, accountingBenchmarkEvents(credential, 0, 4)); err != nil {
		t.Fatal("authoritative settlement waited behind diagnostic retention", err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 120, subStatusActive)
	releaseOnce()
	select {
	case got := <-done:
		if got.err != nil || got.result.FlowStarts != 1 {
			t.Fatal("bounded maintenance failed after concurrent commits", got)
		}
		// Keep the deferred drain nonblocking after this successful receive.
		done <- got
	case <-ctx.Done():
		t.Fatal("maintenance did not finish", ctx.Err())
	}
}
