package handler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestMySQLColdPreparationDoesNotBlockOrOverwriteHigherHistoricalReport(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	db := h.db
	if e := db.Model(&model.ProtocolEndpoint{}).Where("id=?", c.ProtocolEndpointID).Update("multiplier_milli", 1500).Error; e != nil {
		t.Fatal(e)
	}
	base := time.Now().UTC().Truncate(time.Millisecond)
	old := base.Add(-48 * time.Hour)
	clock := old
	db.Config.NowFunc = func() time.Time { return clock }
	event := metering.CompletedFlow{NodeID: c.NodeID, PrincipalKey: c.PrincipalKey, CoreInstanceID: "cold-race", FlowID: "same-flow", EventID: "original", EventType: "flow.completed", Sequence: 1, Revision: 1, BytesUp: 3, OccurredAt: old}
	pay := func(e metering.CompletedFlow) error {
		_, _, err := h.services.CompletionAccounting(h.credentialCipher).Complete(t.Context(), e)
		return err
	}
	if e := pay(event); e != nil {
		t.Fatal(e)
	}
	now := base.Add(3 * 24 * time.Hour)
	compact := func() {
		t.Helper()
		if _, e := (meteringstore.FlowCursorCompaction{DB: db, Now: func() time.Time { return now }}).Drain(t.Context(), 100); e != nil {
			t.Fatal(e)
		}
	}
	compact()
	if _, e := (meteringstore.UsageProjectionWorker{DB: db}).Drain(t.Context(), 100); e != nil {
		t.Fatal(e)
	}
	if _, e := (meteringstore.LedgerRetention{DB: db, Now: func() time.Time { return now }}).Drain(t.Context(), 100); e != nil {
		t.Fatal(e)
	}
	if _, e := (meteringstore.ColdIdentities{DB: db, Now: func() time.Time { return now }}).Drain(t.Context(), 32); e != nil {
		t.Fatal(e)
	}
	clock = now
	event.EventID = "second"
	event.Sequence, event.Revision, event.BytesUp = 2, 2, 6
	if e := pay(event); e != nil {
		t.Fatal(e)
	}
	now = now.Add(2 * time.Hour)
	compact()
	ready, release := make(chan struct{}), make(chan struct{})
	var once atomic.Bool
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	const hook = "test:pause_cold_read_only_prepare"
	db.Callback().Query().After("gorm:query").Register(hook, func(tx *gorm.DB) {
		_, locked := tx.Statement.Clauses["FOR"]
		if tx.Statement.Table == "flow_cursor_checkpoints" && !locked && tx.RowsAffected > 0 && once.CompareAndSwap(false, true) {
			close(ready)
			select {
			case <-release:
			case <-ctx.Done():
				tx.AddError(ctx.Err())
			}
		}
	})
	defer db.Callback().Query().Remove(hook)
	done := make(chan error, 1)
	go func() {
		_, e := (meteringstore.ColdIdentities{DB: db, Now: func() time.Time { return now }}).Drain(ctx, 32)
		done <- e
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("cold transfer did not reach unlocked preparation", ctx.Err())
	}
	// A real node writer and higher historical completion both finish while the
	// old floor is still being prepared. Neither can wait behind its gzip work.
	probe := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n model.Node
		return tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "NOWAIT"}).First(&n, c.NodeID).Error
	})
	event.EventID = "third"
	event.Sequence, event.Revision, event.BytesUp = 3, 3, 9
	higher := pay(event)
	close(release)
	prepareErr := <-done
	if probe != nil || higher != nil || prepareErr != nil {
		t.Fatal(probe, higher, prepareErr)
	}
	event.EventID = "fourth"
	event.Sequence, event.Revision, event.BytesUp = 4, 4, 12
	for range 2 {
		if e := pay(event); e != nil {
			t.Fatal("higher floor overwritten by stale cold plan", e)
		}
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 20, subStatusActive)
	var sum int64
	if e := db.Model(&model.MeteringSettlementResult{}).Select("COALESCE(SUM(used_bytes),0)").Scan(&sum).Error; e != nil || sum != 20 {
		t.Fatal("result/quota diverged", sum, e)
	}
	t.Log("cold preparation allowed live node write and old higher report; stale plan did not overwrite cumulative floor; 4 x ceil(3*1.5)=20")
}

func TestMySQLLedgerRetentionSkipsBusyQuotaBeforeLedgerOwnership(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	base := time.Now().UTC().Truncate(time.Millisecond)
	old := base.Add(-48 * time.Hour)
	h.db.NowFunc = func() time.Time { return old }
	e := metering.CompletedFlow{NodeID: c.NodeID, PrincipalKey: c.PrincipalKey, CoreInstanceID: "retention-busy", FlowID: "old-flow", EventID: "old-report", EventType: "flow.completed", Sequence: 1, BytesUp: 7, OccurredAt: old}
	if _, _, err := h.services.CompletionAccounting(h.credentialCipher).Complete(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	if _, err := (meteringstore.UsageProjectionWorker{DB: h.db}).Drain(t.Context(), 100); err != nil {
		t.Fatal(err)
	}
	held := h.db.Begin()
	defer held.Rollback()
	var sub model.Subscription
	if err := held.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, c.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	now := base.Add(3 * 24 * time.Hour)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	n, err := (meteringstore.LedgerRetention{DB: h.db, Now: func() time.Time { return now }}).Drain(ctx, 100)
	held.Rollback()
	if err != nil || n != 0 {
		t.Fatal("busy quota became a failed retention transaction", n, err)
	}
	// Source remains intact; the normal next pass can fold it once the quota is free.
	now = now.Add(2 * time.Hour)
	n, err = (meteringstore.LedgerRetention{DB: h.db, Now: func() time.Time { return now }}).Drain(t.Context(), 100)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, _, err := h.services.CompletionAccounting(h.credentialCipher).Complete(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	var after model.Subscription
	if err := h.db.First(&after, c.SubscriptionID).Error; err != nil || after.FlowUsed != 7 {
		t.Fatal("retention/replay changed billed quota", after.FlowUsed, err)
	}
}
