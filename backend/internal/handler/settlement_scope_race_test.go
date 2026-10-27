package handler

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type settlementPreparationContextKey struct{}

// Pause after real metadata discovery outside an authority transaction. The
// competing writer can commit without relying on sleeps or SQLite row locks.
func pauseSettlementPreparation(t *testing.T, db *gorm.DB, ctx context.Context) (<-chan struct{}, func(), *atomic.Int32) {
	t.Helper()
	const callback = "test:settlement-source-preparation-barrier"
	ready, release := make(chan struct{}), make(chan struct{})
	var reached atomic.Bool
	var reads atomic.Int32
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Error != nil || tx.Statement.Context.Value(settlementPreparationContextKey{}) != true || tx.Statement.Table != "orders" || !strings.Contains(strings.Join(tx.Statement.Selects, " "), "evidence_bytes") || tx.RowsAffected == 0 {
			return
		}
		reads.Add(1)
		if !reached.CompareAndSwap(false, true) {
			return
		}
		if _, inTransaction := tx.Statement.ConnPool.(*sql.Tx); inTransaction {
			tx.AddError(errors.New("preparation barrier entered authority transaction"))
			return
		}
		close(ready)
		select {
		case <-release:
		case <-ctx.Done():
			tx.AddError(ctx.Err())
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unblock(); db.Callback().Query().Remove(callback) })
	return ready, unblock, &reads
}

func settlementScopeSQLiteFixture(t *testing.T) orderFixture {
	f := newOrderFixture(t)
	prepareTestMaintenanceSource(t, f.h)
	return f
}

func TestAutomaticRenewalAndVerifiedCashReprepareStaleScopeBothWinnerOrders(t *testing.T) {
	assertSettlementScopeWinnerOrders(t, settlementScopeSQLiteFixture)
}

func TestMySQLAutomaticRenewalAndVerifiedCashReprepareStaleScopeBothWinnerOrders(t *testing.T) {
	assertSettlementScopeWinnerOrders(t, newMySQLBusinessOrderFixture)
}

func assertSettlementScopeWinnerOrders(t *testing.T, fixture func(*testing.T) orderFixture) {
	for _, autoFirst := range []bool{true, false} {
		name := "manual_wins"
		if autoFirst {
			name = "automatic_wins"
		}
		t.Run(name, func(t *testing.T) {
			f := expiredRecoveryFixture(t, fixture(t))
			setRecoveryPolicy(t, f, 14)
			service := automaticRenewalsFor(f)
			enableAutomaticRenewal(t, f, service)
			pending, err := f.orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.base.skuRecord.ID, TargetSubscriptionID: f.subscription})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.payments.Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: false}); err != nil {
				t.Fatal(err)
			}
			facts := receiptServiceForScope(f)
			authority := commerce.PaymentFactAuthority{AccountID: 1, Provider: "scope-auto-cash-race", RequireAdministrator: true}
			fact := commerce.PaymentFact{OrderID: pending.ID, ProviderEventID: "scope-race-cash", ProviderTradeNo: "scope-race-cash", Status: "paid", AmountMinor: 12000, Currency: "CNY", OccurredAt: *f.at}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			ready, unblock, reads := pauseSettlementPreparation(t, f.db, ctx)
			type outcome struct {
				fact      commerce.PaymentFactResult
				processed int
				err       error
			}
			done := make(chan outcome, 1)
			tagged := context.WithValue(ctx, settlementPreparationContextKey{}, true)
			go func() {
				if autoFirst {
					result, err := facts.Record(tagged, authority, fact)
					done <- outcome{fact: result, err: err}
				} else {
					n, err := service.Process(tagged, 20)
					done <- outcome{processed: n, err: err}
				}
			}()
			joined := false
			defer func() {
				unblock()
				cancel()
				if !joined {
					<-done
				}
			}()
			select {
			case <-ready:
			case got := <-done:
				joined = true
				t.Fatal("losing operation missed outside scope barrier", got.err)
			case <-ctx.Done():
				t.Fatal("outside scope preparation did not complete", ctx.Err())
			}
			var cash commerce.PaymentFactResult
			if autoFirst {
				if n, err := service.Process(ctx, 20); err != nil || n != 1 {
					t.Fatal("automatic winner did not commit", n, err)
				}
			} else {
				cash, err = facts.Record(ctx, authority, fact)
				if err != nil || !cash.Fulfilled || cash.Disposition != "fulfilled" {
					t.Fatal("manual winner did not commit", cash, err)
				}
			}
			unblock()
			got := <-done
			joined = true
			if got.err != nil {
				t.Fatal("losing operation rejected pre-financial scope drift", got.err)
			}
			if autoFirst {
				cash = got.fact
				if cash.Fulfilled || cash.Disposition != "wallet_credit" || cash.Status != "canceled" || reads.Load() < 2 {
					t.Fatal("actual late cash was not durably credited after fresh preparation", cash, reads.Load())
				}
			} else if got.processed != 0 {
				t.Fatal("automatic loser charged an already recovered subscription", got.processed)
			}
			for range 2 {
				replay, err := facts.Record(ctx, authority, fact)
				if err != nil || replay.Disposition != cash.Disposition || replay.Status != cash.Status || replay.Fulfilled {
					t.Fatal("receipt replay changed the committed financial outcome", replay, cash, err)
				}
			}
			assertOneRecoveryAndCashIdentity(t, f, authority.Provider, pending.ID, 18000)
		})
	}
}

func receiptServiceForScope(f fullReplacementFixture) commerce.PaymentFacts {
	return commerce.PaymentFacts{Repository: commercestore.PaymentFacts{DB: f.db, Issuer: zeroadapter.Issuer{Cipher: f.base.h.credentialCipher, Mieru: f.base.h.zeroMieruAccess}, Now: func() time.Time { return *f.at }}}
}

func assertOneRecoveryAndCashIdentity(t *testing.T, f fullReplacementFixture, provider string, cashOrder uint, balance int64) {
	t.Helper()
	var sub model.Subscription
	if err := f.db.First(&sub, f.subscription).Error; err != nil || sub.EntitlementEpoch != 1 || sub.Status != "active" || !sub.StartAt.Equal(*f.at) || !sub.EndAt.Equal(f.at.AddDate(1, 0, 0)) {
		t.Fatal("scope race changed the restored term twice", sub, err)
	}
	var wallet model.WalletAccount
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != balance || wallet.ReservedMinor != 0 {
		t.Fatal("scope race lost or duplicated wallet/cash", wallet, err)
	}
	for _, query := range []struct {
		model any
		where string
		args  []any
	}{
		{&model.Order{}, "subscription_id = ? AND order_type = ? AND status = ?", []any{f.subscription, "renewal", "paid"}},
		{&model.PaymentEvent{}, "order_id = ? AND provider = ?", []any{cashOrder, provider}},
		{&model.PaymentReceipt{}, "order_id = ? AND provider = ?", []any{cashOrder, provider}},
		{&model.ClosedEntitlement{}, "subscription_id = ?", []any{f.subscription}},
	} {
		var n int64
		if err := f.db.Model(query.model).Where(query.where, query.args...).Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("financial identity or recovery duplicated: model=%T count=%d err=%v", query.model, n, err)
		}
	}
}

func TestCanceledClosedSeriesStillAcceptsLateCashWithoutRevival(t *testing.T) {
	assertCanceledClosedSeriesCash(t, settlementScopeSQLiteFixture(t))
}

func TestMySQLCanceledClosedSeriesStillAcceptsLateCashWithoutRevival(t *testing.T) {
	assertCanceledClosedSeriesCash(t, newMySQLBusinessOrderFixture(t))
}

func assertCanceledClosedSeriesCash(t *testing.T, base orderFixture) {
	f := expiredRecoveryFixture(t, base)
	setRecoveryPolicy(t, f, 14)
	service := automaticRenewalsFor(f)
	enableAutomaticRenewal(t, f, service)
	pending, err := f.orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.base.skuRecord.ID, TargetSubscriptionID: f.subscription})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := (commercestore.OrderCancellation{DB: f.db}).Cancel(t.Context(), 1, pending.ID, false, false); err != nil {
		t.Fatal(err)
	}
	if n, err := service.Process(t.Context(), 20); err != nil || n != 1 {
		t.Fatal("automatic recovery failed", n, err)
	}
	var old model.Order
	var sub model.Subscription
	if err := f.db.First(&old, pending.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&sub, f.subscription).Error; err != nil || old.Status != "canceled" || old.SubscriptionEndedAt == nil || old.SubscriptionEndedAt.After(sub.StartAt) {
		t.Fatal("fixture did not close the canceled attempt before the current source boundary", old, sub, err)
	}
	// Prove it is absent from the production source predicate, rather than only
	// testing an ordinary canceled row that is still part of the current scope.
	var currentSource int64
	if err := f.db.Model(&model.Order{}).Where("id = ?", old.ID).Where("subscription_ended_at IS NULL OR subscription_ended_at > ? OR status = ? OR (status IN ? AND EXISTS (SELECT 1 FROM order_fundings WHERE order_fundings.order_id = orders.id AND order_fundings.state = ?))", sub.StartAt, "pending", []string{"canceled", "failed"}, "reserved").Count(&currentSource).Error; err != nil || currentSource != 0 {
		t.Fatal("old canceled order still belonged to current source set", currentSource, err)
	}
	facts := receiptServiceForScope(f)
	authority := commerce.PaymentFactAuthority{AccountID: 1, Provider: "closed-series-cash", RequireAdministrator: true}
	fact := commerce.PaymentFact{OrderID: old.ID, ProviderEventID: "closed-series-late", ProviderTradeNo: "closed-series-late", Status: "paid", AmountMinor: 12000, Currency: "CNY", OccurredAt: *f.at}
	for range 3 {
		result, err := facts.Record(t.Context(), authority, fact)
		if err != nil || result.Fulfilled || result.Disposition != "wallet_credit" || result.Status != "canceled" {
			t.Fatal("closed/canceled cash was rejected, duplicated, or revived rights", result, err)
		}
	}
	assertOneRecoveryAndCashIdentity(t, f, authority.Provider, old.ID, 18000)
}
