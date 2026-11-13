package handler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestMySQLColdCommitDoesNotBlockNodeOrWalletPayment(t *testing.T) {
	base := newMySQLBusinessOrderFixture(t)
	h, c := accountingFixtureFromOrder(t, base)
	event := metering.CompletedFlow{NodeID: c.NodeID, PrincipalKey: c.PrincipalKey, CoreInstanceID: "cold-payment", FlowID: "old", EventID: "old", EventType: "flow.completed", Sequence: 1, BytesUp: 7, OccurredAt: time.Now().UTC().Add(-48 * time.Hour)}
	if _, _, e := h.services.CompletionAccounting(h.credentialCipher).Complete(t.Context(), event); e != nil {
		t.Fatal(e)
	}
	if _, e := (meteringstore.UsageProjectionWorker{DB: h.db}).Drain(t.Context(), 100); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC().Add(4 * 24 * time.Hour)
	if _, e := (meteringstore.LedgerRetention{DB: h.db, Now: func() time.Time { return now }}).Drain(t.Context(), 100); e != nil {
		t.Fatal(e)
	}
	fundTestWallet(t, base, 1000)
	order := base.create(t, 0)
	payment := fundingService(base)
	if _, e := payment.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); e != nil {
		t.Fatal(e)
	}
	ready, release := make(chan struct{}), make(chan struct{})
	var entered atomic.Bool
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	const hook = "test:cold-commit-fence"
	if e := h.db.Callback().Query().After("gorm:query").Register(hook, func(q *gorm.DB) {
		l, ok := q.Statement.Clauses["FOR"].Expression.(clause.Locking)
		if q.Error == nil && q.Statement.Table == "metering_identity_fences" && ok && l.Strength == "UPDATE" && entered.CompareAndSwap(false, true) {
			close(ready)
			select {
			case <-release:
			case <-ctx.Done():
				q.AddError(ctx.Err())
			}
		}
	}); e != nil {
		t.Fatal(e)
	}
	defer h.db.Callback().Query().Remove(hook)
	done := make(chan error, 1)
	go func() {
		_, e := (meteringstore.ColdIdentities{DB: h.db, Now: func() time.Time { return now }}).Drain(ctx, 32)
		done <- e
	}()
	select {
	case <-ready:
	case e := <-done:
		t.Fatal("cold commit did not acquire its own fence", e)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	probe := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node model.Node
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "NOWAIT"}).First(&node, c.NodeID).Error; e != nil {
			return e
		}
		return tx.Model(&node).UpdateColumn("last_seen_at", time.Now().UTC()).Error
	})
	started := time.Now()
	_, paymentErr := payment.Pay(ctx, 1, order.ID)
	duration := time.Since(started)
	close(release)
	coldErr := <-done
	if probe != nil || paymentErr != nil || coldErr != nil {
		t.Fatal(probe, paymentErr, coldErr)
	}
	if duration > time.Second {
		t.Fatal("payment waited behind identity conversion", duration)
	}
	walletMatches(t, base, 1000-order.PayableAmount, 0, 3)
	if _, e := payment.Pay(t.Context(), 1, order.ID); e != nil {
		t.Fatal(e)
	}
	walletMatches(t, base, 1000-order.PayableAmount, 0, 3)
	t.Logf("wallet payment completed in %s while cold commit held the identity fence; repeat captured no extra funds", duration)
}

func TestMySQLHistoricalQuotaDoesNotWaitForCurrentSubscription(t *testing.T) {
	f := newFullReplacementFixture(t, newMySQLBusinessOrderFixture(t), false, 80, 2)
	order := confirmedReplacement(t, f, "historical-isolation")
	if _, e := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.payments.Pay(t.Context(), 1, order.ID); e != nil {
		t.Fatal(e)
	}
	lock := f.db.Begin()
	defer lock.Rollback()
	var current model.Subscription
	if e := lock.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, f.subscription).Error; e != nil {
		t.Fatal(e)
	}
	currentUsedBefore := current.FlowUsed
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	event := metering.CompletedFlow{NodeID: f.oldCredential.NodeID, PrincipalKey: f.oldCredential.PrincipalKey, CoreInstanceID: "old-isolated", FlowID: "newly-arrived-old-flow", EventID: "historical-isolated", EventType: "flow.completed", Sequence: 1, BytesDown: 30, OccurredAt: f.at.Add(time.Second)}
	for range 2 {
		if record, _, e := f.base.h.services.CompletionAccounting(f.base.h.credentialCipher).Complete(ctx, event); e != nil {
			t.Fatal("historical quota waited on the current epoch", e)
		} else {
			t.Logf("historical charge epoch=%d raw=%d used=%d multiplier=%d calc=%d", record.EntitlementEpoch, record.RawBytes, record.UsedBytes, record.ProtocolMultiplierMilli, record.TrafficCalcMode)
		}
	}
	// A higher old cumulative report must resume the compact floor, never create
	// a hot row whose subscription FK would reacquire the current quota lock.
	event.EventID = "historical-higher"
	event.Sequence = 2
	event.BytesDown = 40
	if _, _, e := f.base.h.services.CompletionAccounting(f.base.h.credentialCipher).Complete(ctx, event); e != nil {
		t.Fatal(e)
	}
	if e := lock.Commit().Error; e != nil {
		t.Fatal(e)
	}
	var closed model.ClosedEntitlement
	if e := f.db.Where("subscription_id=? AND epoch=0", f.subscription).First(&closed).Error; e != nil || closed.FlowUsed != 100 {
		t.Fatal(closed.FlowUsed, e)
	}
	if e := f.db.First(&current, f.subscription).Error; e != nil || current.FlowUsed != currentUsedBefore {
		t.Fatal(current.FlowUsed, e)
	}
}

func TestMySQLWalletPaymentRetriesShortNodeWriteWithoutDuplicateCapture(t *testing.T) {
	base := newMySQLBusinessOrderFixture(t)
	attachOrderPublishEndpoint(t, base)
	fundTestWallet(t, base, 1000)
	order := base.create(t, 0)
	payment := fundingService(base)
	if _, e := payment.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); e != nil {
		t.Fatal(e)
	}
	var endpoint model.ProtocolEndpoint
	if e := base.h.db.First(&endpoint).Error; e != nil {
		t.Fatal(e)
	}
	lock := base.h.db.Begin()
	defer lock.Rollback()
	var node model.Node
	if e := lock.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, endpoint.NodeID).Error; e != nil {
		t.Fatal(e)
	}
	failed := make(chan struct{})
	var once atomic.Bool
	const hook = "test:wallet-transient-node-write"
	if e := base.h.db.Callback().Query().After("gorm:query").Register(hook, func(q *gorm.DB) {
		if q.Statement.Table == "nodes" && q.Error != nil && once.CompareAndSwap(false, true) {
			close(failed)
		}
	}); e != nil {
		t.Fatal(e)
	}
	defer base.h.db.Callback().Query().Remove(hook)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := payment.Pay(ctx, 1, order.ID); done <- e }()
	select {
	case <-failed:
	case e := <-done:
		t.Fatal("payment did not encounter held node", e)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if e := lock.Commit().Error; e != nil {
		t.Fatal(e)
	}
	if e := <-done; e != nil {
		t.Fatal("short node write leaked as failed payment", e)
	}
	for range 2 {
		if _, e := payment.Pay(t.Context(), 1, order.ID); e != nil {
			t.Fatal(e)
		}
	}
	walletMatches(t, base, 1000-order.PayableAmount, 0, 3)
}
