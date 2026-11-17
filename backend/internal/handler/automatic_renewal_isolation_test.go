package handler

import (
	"strconv"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestAutomaticRenewalOverBoundItemKeepsNeighborsAndRevocationAvailable(t *testing.T) {
	base := newOrderFixture(t)
	prepareTestMaintenanceSource(t, base.h)
	assertAutomaticOverBoundIsolation(t, base, false)
}
func TestMySQLAutomaticRenewalOverBoundItemKeepsNeighborsAndRevocationAvailable(t *testing.T) {
	assertAutomaticOverBoundIsolation(t, newMySQLBusinessOrderFixture(t), false)
}
func TestAutomaticRenewalOverBoundWindowStillCloses(t *testing.T) {
	base := newOrderFixture(t)
	prepareTestMaintenanceSource(t, base.h)
	assertAutomaticOverBoundIsolation(t, base, true)
}
func TestMySQLAutomaticRenewalOverBoundWindowStillCloses(t *testing.T) {
	assertAutomaticOverBoundIsolation(t, newMySQLBusinessOrderFixture(t), true)
}

func TestAutomaticRenewalConsentWithdrawalRace(t *testing.T) {
	base := newOrderFixture(t)
	prepareTestMaintenanceSource(t, base.h)
	assertAutomaticConsentWithdrawalRace(t, base)
}
func TestMySQLAutomaticRenewalConsentWithdrawalRace(t *testing.T) {
	assertAutomaticConsentWithdrawalRace(t, newMySQLBusinessOrderFixture(t))
}
func assertAutomaticConsentWithdrawalRace(t *testing.T, base orderFixture) {
	f := expiredRecoveryFixture(t, base)
	setRecoveryPolicy(t, f, 14)
	service := automaticRenewalsFor(f)
	consent := enableAutomaticRenewal(t, f, service)
	start := make(chan struct{})
	done := make(chan error, 2)
	go func() { <-start; _, err := service.Process(t.Context(), 20); done <- err }()
	go func() {
		<-start
		_, err := service.Set(t.Context(), 1, f.subscription, commerce.AutomaticRenewalRequest{ExpectedRevision: consent.Revision})
		done <- err
	}()
	close(start)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal("consent withdrawal race failed", err)
		}
	}
	view, err := service.Read(t.Context(), 1, f.subscription, 0)
	if err != nil || view.Enabled || view.State != "disabled" {
		t.Fatalf("withdrawn consent was restored: enabled=%t state=%s err=%v", view.Enabled, view.State, err)
	}
	if n, err := service.Process(t.Context(), 20); err != nil || n != 0 {
		t.Fatal("withdrawn consent was charged again", n, err)
	}
	var sub model.Subscription
	var wallet model.WalletAccount
	if err := f.db.First(&sub, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil {
		t.Fatal(err)
	}
	if wallet.ReservedMinor != 0 || !((sub.EntitlementEpoch == 0 && wallet.BalanceMinor == 18000) || (sub.EntitlementEpoch == 1 && wallet.BalanceMinor == 6000)) {
		t.Fatalf("race did not produce one serial financial outcome: epoch=%d balance=%d reserved=%d", sub.EntitlementEpoch, wallet.BalanceMinor, wallet.ReservedMinor)
	}
}
func assertAutomaticOverBoundIsolation(t *testing.T, base orderFixture, closeWindow bool) {
	f := newFullReplacementFixtureInYear(t, base, false, 0, 0, 2025)
	issuer := zeroadapter.Issuer{Cipher: base.h.credentialCipher, Mieru: base.h.zeroMieruAccess}
	pending, err := f.orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: base.skuRecord.ID})
	if err != nil {
		t.Fatal(err)
	}
	facts := commerce.PaymentFacts{Repository: commercestore.PaymentFacts{DB: f.db, Issuer: issuer, Now: func() time.Time { return *f.at }}}
	paid, err := facts.Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: "sandbox-auto-isolation", RequireAdministrator: true}, commerce.PaymentFact{OrderID: pending.ID, ProviderEventID: "second-independent-purchase", ProviderTradeNo: "second-independent-purchase", Status: "paid", AmountMinor: 12000, Currency: "CNY", OccurredAt: *f.at})
	if err != nil || !paid.Fulfilled {
		t.Fatal(paid, err)
	}
	var second model.Order
	if err := f.db.First(&second, pending.ID).Error; err != nil {
		t.Fatal(err)
	}
	*f.at = time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC)
	setRecoveryPolicy(t, f, 14)
	service := automaticRenewalsFor(f)
	consent := enableAutomaticRenewal(t, f, service)
	other := f
	other.subscription = second.SubscriptionID
	enableAutomaticRenewal(t, other, service)
	rows := make([]model.Order, 1001)
	for i := range rows {
		rows[i] = model.Order{UserID: 1, PlanID: base.planRecord.ID, PlanSKUID: base.skuRecord.ID, TradeNo: "isolated-canceled-" + strconv.Itoa(i), OrderType: "renewal", TargetSubscriptionID: &f.subscription, Status: "canceled", Currency: "CNY", SubscriptionEndedAt: f.at}
	}
	if err := f.db.CreateInBatches(&rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	if view, err := service.Read(t.Context(), 1, f.subscription, 0); err != nil || !view.Enabled {
		t.Errorf("over-bound history hid the existing consent/revocation state: enabled=%t err=%v", view.Enabled, err)
	}
	if count, err := service.Process(t.Context(), 20); err != nil || count != 2 {
		t.Errorf("one over-bound item blocked its neighbor: count=%d err=%v", count, err)
	}
	var paused, renewed model.SubscriptionAutomaticRenewal
	if err := f.db.First(&paused, "subscription_id = ?", f.subscription).Error; err != nil || paused.State != "history_review_required" || paused.LastOrderID != nil || paused.AttemptSequence != 1 {
		t.Errorf("missing durable non-charge review state: state=%s sequence=%d err=%v", paused.State, paused.AttemptSequence, err)
	}
	if err := f.db.First(&renewed, "subscription_id = ?", other.subscription).Error; err != nil || renewed.State != "renewed" || renewed.AttemptSequence != 2 {
		t.Errorf("neighbor did not retain stable queue order: state=%s sequence=%d err=%v", renewed.State, renewed.AttemptSequence, err)
	}
	queries := entitlements.SubscriptionQueries{Repository: entitlementstore.SubscriptionQueries{DB: f.db, Now: func() time.Time { return *f.at }}}
	page, err := queries.Owned(t.Context(), 1, entitlements.SubscriptionQuery{Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range page.Items {
		if item.ID == f.subscription {
			found = item.AutomaticRenewalState == "history_review_required" && item.AutomaticRenewalMessage != "" && item.AutomaticRenewalCheckedAt != nil
		}
	}
	if !found {
		t.Error("account list omitted the persisted per-subscription review notice/time")
	}
	if closeWindow {
		*f.at = time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
		if count, err := service.Process(t.Context(), 20); err != nil || count != 1 {
			t.Fatalf("over-bound window cleanup failed: count=%d err=%v", count, err)
		}
		state, err := service.Read(t.Context(), 1, f.subscription, 0)
		if err != nil || state.Enabled || state.State != "window_closed" {
			t.Errorf("history bound bypassed window end: state=%s enabled=%t err=%v", state.State, state.Enabled, err)
		}
	} else if out, err := service.Set(t.Context(), 1, f.subscription, commerce.AutomaticRenewalRequest{ExpectedRevision: consent.Revision}); err != nil || out.Enabled {
		t.Errorf("over-bound history prevented withdrawal of debit consent: enabled=%t err=%v", out.Enabled, err)
	}
	var wallet model.WalletAccount
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 6000 || wallet.ReservedMinor != 0 {
		t.Errorf("isolated item charged or stranded funds: balance=%d reserved=%d err=%v", wallet.BalanceMinor, wallet.ReservedMinor, err)
	}
}
