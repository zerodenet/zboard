package handler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestMySQLReplacementQuoteRejectsCurrentQuotaEventAfterRepeatableReadSnapshot(t *testing.T) {
	f := newFullReplacementFixture(t, newMySQLBusinessOrderFixture(t), false, 0, 3)
	request := commerce.OrderCreateRequest{PlanSKUID: f.target, TargetSubscriptionID: f.subscription, IdempotencyKey: "replacement-current-quota-event"}
	quoted, err := f.orders.Preview(t.Context(), 1, request)
	if err != nil || quoted.SettlementAmount != 9000 {
		t.Fatal("fixture quote", quoted, err)
	}
	request.QuoteFingerprint = quoted.QuoteFingerprint
	order, err := f.orders.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	var oldEvent struct{ ID uint }
	if err := f.db.Model(&model.QuotaEvent{}).Where("subscription_id = ?", f.subscription).Select("COALESCE(MAX(id),0) AS id").Scan(&oldEvent).Error; err != nil {
		t.Fatal(err)
	}
	var walletBefore model.WalletAccount
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&walletBefore).Error; err != nil {
		t.Fatal(err)
	}

	established, resume := make(chan struct{}), make(chan struct{})
	var resumeOnce sync.Once
	release := func() { resumeOnce.Do(func() { close(resume) }) }
	var paused atomic.Bool
	var snapshotEvent uint
	payCtx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	started, received := false, false
	const callback = "test:replacement-current-quota-event"
	if err := f.db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Error != nil || tx.Statement.Table != "subscriptions" || strings.Join(tx.Statement.Selects, ",") != "id,start_at" {
			return
		}
		if _, locking := tx.Statement.Clauses["FOR"]; locking {
			return
		}
		if _, transaction := tx.Statement.ConnPool.(gorm.TxCommitter); !transaction || !paused.CompareAndSwap(false, true) {
			return
		}
		// This is a real financial transaction after source/account acquisition.
		// Check the transaction itself, not another pooled connection's defaults.
		rows, err := tx.Statement.ConnPool.QueryContext(tx.Statement.Context, "SELECT @@transaction_isolation")
		if err != nil {
			tx.AddError(err)
			return
		}
		var isolation string
		if !rows.Next() {
			tx.AddError(errors.New("missing transaction isolation observation"))
		} else if err := rows.Scan(&isolation); err != nil {
			tx.AddError(err)
		}
		if err := rows.Close(); err != nil {
			tx.AddError(err)
		}
		if tx.Error != nil {
			return
		}
		if isolation != "REPEATABLE-READ" {
			tx.AddError(errors.New("regression requires actual MySQL REPEATABLE READ"))
			return
		}
		close(established)
		select {
		case <-resume:
		case <-tx.Statement.Context.Done():
			tx.AddError(tx.Statement.Context.Err())
			return
		}
		// Demonstrate the failure mode: the ordinary MAX still observes the old
		// snapshot after the independent quota writer has actually committed.
		var old struct{ ID uint }
		if err := tx.Session(&gorm.Session{NewDB: true}).Model(&model.QuotaEvent{}).Where("subscription_id = ?", f.subscription).Select("COALESCE(MAX(id),0) AS id").Scan(&old).Error; err != nil {
			tx.AddError(err)
			return
		}
		snapshotEvent = old.ID
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		release()
		if started && !received {
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Error("financial transaction did not stop after barrier cleanup")
			}
		}
		if err := f.db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	started = true
	go func() {
		_, err := f.payments.Pay(payCtx, 1, order.ID)
		done <- err
	}()
	select {
	case <-established:
	case err := <-done:
		received = true
		t.Fatalf("financial transaction did not establish the expected snapshot: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("financial transaction did not reach its snapshot barrier")
	}

	// The quota worker's authority boundary is subscription-only; it must not
	// need the buyer lock already held by the paused payment. Unused quota can
	// change without changing rounded remaining money, so value comparison
	// alone cannot detect that the previously accepted rights have changed.
	adjust, stopAdjust := context.WithTimeout(t.Context(), 5*time.Second)
	defer stopAdjust()
	if err := f.db.WithContext(adjust).Transaction(func(tx *gorm.DB) error {
		var sub model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, f.subscription).Error; err != nil {
			return err
		}
		before := sub.FlowTotal - sub.FlowUsed
		if err := tx.Model(&sub).Update("flow_total", sub.FlowTotal+1).Error; err != nil {
			return err
		}
		return entitlementstore.RecordQuotaEvent(tx, sub, "task_adjustment", 1, before, before+1, "test_concurrent_quota", "replacement-current-quota-event")
	}); err != nil {
		t.Fatal("subscription-only quota transaction could not commit", err)
	}
	var committedEvent struct{ ID uint }
	if err := f.db.Model(&model.QuotaEvent{}).Where("subscription_id = ?", f.subscription).Select("COALESCE(MAX(id),0) AS id").Scan(&committedEvent).Error; err != nil || committedEvent.ID <= oldEvent.ID {
		t.Fatal("quota event did not actually commit", committedEvent, err)
	}
	release()
	select {
	case err = <-done:
		received = true
	case <-time.After(10 * time.Second):
		t.Fatal("payment did not finish after quota commit")
	}
	var invalid *commerce.ValidationError
	if !errors.As(err, &invalid) || !strings.Contains(invalid.Message, "可结算金额或来源已变化") {
		t.Fatalf("stale accepted quote fulfilled despite a current quota event: %v", err)
	}
	if snapshotEvent != oldEvent.ID {
		t.Fatalf("test did not reproduce the stale RR observation: got=%d old=%d committed=%d", snapshotEvent, oldEvent.ID, committedEvent.ID)
	}
	fresh, err := f.orders.Preview(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.target, TargetSubscriptionID: f.subscription})
	if err != nil || fresh.SettlementAmount != quoted.SettlementAmount || fresh.CreditAmount != quoted.CreditAmount || fresh.SurplusAmount != quoted.SurplusAmount || fresh.QuoteFingerprint == quoted.QuoteFingerprint {
		t.Fatal("fixture changed rounded money instead of only the accepted rights", fresh, err)
	}
	var after model.Subscription
	if err := f.db.First(&after, f.subscription).Error; err != nil || after.PlanID != f.base.planRecord.ID || after.Status != "active" || after.FlowTotal != 101 {
		t.Fatal("failed payment changed the externally adjusted subscription", after, err)
	}
	var walletAfter model.WalletAccount
	if err := f.db.Where("user_id = ? AND currency = ?", walletBefore.UserID, walletBefore.Currency).First(&walletAfter).Error; err != nil || walletAfter.BalanceMinor != walletBefore.BalanceMinor || walletAfter.ReservedMinor != walletBefore.ReservedMinor || walletAfter.Revision != walletBefore.Revision {
		t.Fatal("failed quote validation changed financial balances", walletAfter, err)
	}
	var saved model.Order
	if err := f.db.First(&saved, order.ID).Error; err != nil || saved.Status != "pending" || saved.PaidAmount != 0 {
		t.Fatal("stale quote was partially fulfilled", saved, err)
	}
	var source model.Order
	if err := f.db.First(&source, f.root.ID).Error; err != nil || source.RefundAmount != 0 || source.PaidAmount != f.root.PaidAmount {
		t.Fatal("stale quote returned old paid cash", source, err)
	}
	var settlements int64
	if err := f.db.Model(&model.SubscriptionValueSettlement{}).Where("subscription_id = ?", f.subscription).Count(&settlements).Error; err != nil || settlements != 0 {
		t.Fatal("stale quote committed a value settlement", settlements, err)
	}
}
