package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func automaticRenewalsFor(f fullReplacementFixture) commerce.AutomaticRenewals {
	return commerce.AutomaticRenewals{Repository: commercestore.AutomaticRenewals{DB: f.db, Issuer: zeroadapter.Issuer{Cipher: f.base.h.credentialCipher, Mieru: f.base.h.zeroMieruAccess}, Now: func() time.Time { return *f.at }}}
}

func enableAutomaticRenewal(t *testing.T, f fullReplacementFixture, service commerce.AutomaticRenewals) commerce.AutomaticRenewalView {
	t.Helper()
	view, err := service.Read(t.Context(), 1, f.subscription, f.base.skuRecord.ID)
	if err != nil || view.Enabled || view.Offer == nil || view.Offer.BasePriceMinor != 12000 {
		t.Fatal("missing explicit purchased-period offer", view, err)
	}
	out, err := service.Set(t.Context(), 1, f.subscription, commerce.AutomaticRenewalRequest{Enabled: true, PlanSKUID: view.Offer.PlanSKUID, QuoteFingerprint: view.Offer.QuoteFingerprint, ExpectedRevision: view.Revision})
	if err != nil || !out.Enabled || out.Revision != view.Revision+1 {
		t.Fatal("could not consent to automatic renewal", out, err)
	}
	return out
}

func TestAutomaticRenewalDefaultOffAndSingleRecovery(t *testing.T) {
	assertAutomaticRenewalRecovery(t, newOrderFixture(t))
}
func TestMySQLAutomaticRenewalDefaultOffAndSingleRecovery(t *testing.T) {
	assertAutomaticRenewalRecovery(t, newMySQLOrderFixture(t))
}
func assertAutomaticRenewalRecovery(t *testing.T, base orderFixture) {
	f := expiredRecoveryFixture(t, base)
	setRecoveryPolicy(t, f, 14)
	service := automaticRenewalsFor(f)
	if n, err := service.Process(t.Context(), 20); err != nil || n != 0 {
		t.Fatal("paid subscription implicitly enabled automatic debit", n, err)
	}
	view := enableAutomaticRenewal(t, f, service)
	if _, err := service.Set(t.Context(), 1, f.subscription, commerce.AutomaticRenewalRequest{ExpectedRevision: view.Revision - 1}); !errors.Is(err, commerce.ErrSubscriptionQuoteChanged) {
		t.Fatal("stale consent revision overwrote current agreement", err)
	}
	if n, err := service.Process(t.Context(), 20); err != nil || n != 1 {
		t.Fatal("automatic recovery failed", n, err)
	}
	if n, err := automaticRenewalsFor(f).Process(t.Context(), 20); err != nil || n != 0 {
		t.Fatal("replay renewed the same term twice", n, err)
	}
	var sub model.Subscription
	var wallet model.WalletAccount
	if err := f.db.First(&sub, f.subscription).Error; err != nil || sub.EntitlementEpoch != 1 || sub.Status != "active" || !sub.StartAt.Equal(*f.at) || !sub.EndAt.Equal(f.at.AddDate(1, 0, 0)) {
		t.Fatal("automatic recovery did not grant one complete period", sub, err)
	}
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 6000 || wallet.ReservedMinor != 0 {
		t.Fatal("automatic debit lost or doubled money", wallet, err)
	}
	next, err := service.Read(t.Context(), 1, f.subscription, 0)
	if err != nil || !next.Enabled || next.State != "renewed" || next.LastOrderID == nil || next.NextAttemptAt == nil || !next.NextAttemptAt.Equal(sub.EndAt) {
		t.Fatal("missing durable result", next, err)
	}
	var paid model.Order
	if err := f.db.First(&paid, *next.LastOrderID).Error; err != nil || paid.Channel != "wallet" || paid.PaidAmount != 12000 || paid.Status != "paid" || paid.EntitlementValueSnapshot == "" {
		t.Fatal("automatic result bypassed ordinary payment evidence", paid, err)
	}
}

func TestAutomaticRenewalShortenedWindowStopsWithoutCharging(t *testing.T) {
	assertAutomaticRenewalClosedWindow(t, newOrderFixture(t))
}
func TestMySQLAutomaticRenewalShortenedWindowStopsWithoutCharging(t *testing.T) {
	assertAutomaticRenewalClosedWindow(t, newMySQLOrderFixture(t))
}
func assertAutomaticRenewalClosedWindow(t *testing.T, base orderFixture) {
	f := expiredRecoveryFixture(t, base)
	setRecoveryPolicy(t, f, 14)
	service := automaticRenewalsFor(f)
	enableAutomaticRenewal(t, f, service)
	setRecoveryPolicy(t, f, 7)
	if n, err := service.Process(t.Context(), 20); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	view, err := service.Read(t.Context(), 1, f.subscription, 0)
	if err != nil || view.Enabled || view.State != "window_closed" || view.LastOrderID != nil {
		t.Fatal("closed recovery window retained debit authority", view, err)
	}
	setRecoveryPolicy(t, f, 14)
	if n, err := service.Process(t.Context(), 20); err != nil || n != 0 {
		t.Fatal("extending policy re-enabled automatic debit", n, err)
	}
	var wallet model.WalletAccount
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 18000 || wallet.ReservedMinor != 0 {
		t.Fatal("closed window charged balance", wallet, err)
	}
}

func TestAutomaticRenewalPublicationFailureRollsBackPaymentAndAttempt(t *testing.T) {
	f := expiredRecoveryFixture(t, newOrderFixture(t))
	setRecoveryPolicy(t, f, 14)
	service := automaticRenewalsFor(f)
	enableAutomaticRenewal(t, f, service)
	if err := f.db.Migrator().DropTable(&model.NodeConfigPublish{}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Process(t.Context(), 20); err == nil {
		t.Fatal("payment committed without durable publication intent")
	}
	var wallet model.WalletAccount
	var sub model.Subscription
	var agreement model.SubscriptionAutomaticRenewal
	var guard model.AutomaticRenewalScan
	if err := f.db.First(&sub, f.subscription).Error; err != nil || sub.EntitlementEpoch != 0 {
		t.Fatal("publication failure granted new service", sub.EntitlementEpoch, err)
	}
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 18000 || wallet.ReservedMinor != 0 {
		t.Fatal("publication failure captured or stranded balance", wallet, err)
	}
	if err := f.db.First(&agreement, "subscription_id = ?", f.subscription).Error; err != nil || agreement.State != "waiting" || agreement.LastAttemptAt != nil || agreement.LastOrderID != nil {
		t.Fatal("publication failure committed a misleading attempt result", agreement, err)
	}
	if err := f.db.First(&guard, 1).Error; err != nil || guard.Sequence != 0 {
		t.Fatal("rolled-back attempt advanced ordering evidence", guard, err)
	}
}

func TestAutomaticRenewalStableWalletCompetitionAndTopUpRetry(t *testing.T) {
	assertAutomaticRenewalCompetition(t, newOrderFixture(t))
}
func TestMySQLAutomaticRenewalStableWalletCompetitionAndTopUpRetry(t *testing.T) {
	assertAutomaticRenewalCompetition(t, newMySQLOrderFixture(t))
}
func assertAutomaticRenewalCompetition(t *testing.T, base orderFixture) {
	f := newFullReplacementFixtureInYear(t, base, false, 0, 0, 2025)
	issuer := zeroadapter.Issuer{Cipher: base.h.credentialCipher, Mieru: base.h.zeroMieruAccess}
	facts := commerce.PaymentFacts{Repository: commercestore.PaymentFacts{DB: f.db, Issuer: issuer, Now: func() time.Time { return *f.at }}}
	authority := commerce.PaymentFactAuthority{AccountID: 1, Provider: "sandbox-auto-renew", RequireAdministrator: true}
	secondOrder, err := f.orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: base.skuRecord.ID, IdempotencyKey: "auto-second-independent-purchase"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := facts.Record(t.Context(), authority, commerce.PaymentFact{OrderID: secondOrder.ID, ProviderEventID: "second-purchase", ProviderTradeNo: "second-purchase", Status: "paid", AmountMinor: 12000, Currency: "CNY", OccurredAt: *f.at})
	if err != nil || !second.Fulfilled {
		t.Fatal(second, err)
	}
	*f.at = time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC)
	setRecoveryPolicy(t, f, 14)
	service := automaticRenewalsFor(f)
	enableAutomaticRenewal(t, f, service)
	other := f
	var paidSecond model.Order
	if err := f.db.First(&paidSecond, secondOrder.ID).Error; err != nil {
		t.Fatal(err)
	}
	other.subscription = paidSecond.SubscriptionID
	enableAutomaticRenewal(t, other, service)
	var wg sync.WaitGroup
	errorsFound := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.Process(t.Context(), 20)
			errorsFound <- err
		}()
	}
	wg.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal("concurrent executors failed", err)
		}
	}
	var first, last model.SubscriptionAutomaticRenewal
	if err := f.db.First(&first, "subscription_id = ?", f.subscription).Error; err != nil || first.State != "renewed" || first.AttemptSequence != 1 {
		t.Fatal("stable first subscription did not win", first, err)
	}
	if err := f.db.First(&last, "subscription_id = ?", other.subscription).Error; err != nil || last.State != "insufficient_balance" || last.AttemptSequence != 2 || !last.Enabled || last.LastOrderID != nil {
		t.Fatal("insufficient balance was not a durable non-charge result", last, err)
	}
	if n, err := service.Process(t.Context(), 20); err != nil || n != 0 {
		t.Fatal("insufficient balance caused a busy retry loop", n, err)
	}
	wallet := commerce.Wallet{Repository: commercestore.Wallet{DB: f.db}}
	topup, err := wallet.CreateTopUp(t.Context(), 1, commerce.TopUpRequest{AmountMinor: 6000, Currency: "CNY", IdempotencyKey: "auto-retry-topup"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := facts.Record(t.Context(), authority, commerce.PaymentFact{OrderID: topup.ID, ProviderEventID: "auto-retry-topup", ProviderTradeNo: "auto-retry-topup", Status: "paid", AmountMinor: 6000, Currency: "CNY", OccurredAt: *f.at}); err != nil {
		t.Fatal(err)
	}
	*f.at = f.at.Add(5*time.Minute + time.Second)
	if n, err := service.Process(t.Context(), 20); err != nil || n != 1 {
		t.Fatal("balance retry failed inside recovery window", n, err)
	}
	var balance model.WalletAccount
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&balance).Error; err != nil || balance.BalanceMinor != 0 || balance.ReservedMinor != 0 {
		t.Fatal("wallet competition or top-up double-counted money", balance, err)
	}
}

func TestAutomaticRenewalMigrationPreservesConsentAndRefusesLostSource(t *testing.T) {
	assertAutomaticRenewalMigration(t, newOrderFixture(t))
}
func TestMySQLAutomaticRenewalMigrationPreservesConsentAndRefusesLostSource(t *testing.T) {
	assertAutomaticRenewalMigration(t, newMySQLOrderFixture(t))
}
func assertAutomaticRenewalMigration(t *testing.T, base orderFixture) {
	f := expiredRecoveryFixture(t, base)
	setRecoveryPolicy(t, f, 14)
	service := automaticRenewalsFor(f)
	enableAutomaticRenewal(t, f, service)
	var saved model.SubscriptionAutomaticRenewal
	if err := f.db.First(&saved, "subscription_id = ?", f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&model.AutomaticRenewalScan{}).Where("id = 1").Update("sequence", 41).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec("DELETE FROM schema_migrations WHERE version = ?", "0051_subscription_automatic_renewals.up.sql").Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := datastore.RunMigrations(f.db); err != nil {
			t.Fatal(err)
		}
	}
	var kept model.SubscriptionAutomaticRenewal
	var guard model.AutomaticRenewalScan
	if err := f.db.First(&kept, "subscription_id = ?", f.subscription).Error; err != nil || !kept.Enabled || kept.Revision != saved.Revision || kept.RootOrderID != saved.RootOrderID || kept.AgreementDigest != saved.AgreementDigest {
		t.Fatal("interrupted/repeated migration changed explicit debit consent", kept, err)
	}
	if err := f.db.First(&guard, 1).Error; err != nil || guard.Sequence != 41 {
		t.Fatal("migration reset durable processing order", guard, err)
	}
	if err := f.db.Migrator().DropTable(&model.SubscriptionAutomaticRenewal{}); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.db); err == nil {
		t.Fatal("lost consent table was silently recreated")
	}
	if f.db.Migrator().HasTable(&model.SubscriptionAutomaticRenewal{}) {
		t.Fatal("startup masked lost debit consent")
	}
}

func TestAutomaticRenewalHTTPRejectsForgedAmountsAndOtherOwners(t *testing.T) {
	f := newFullReplacementFixture(t, newOrderFixture(t), false, 0, 0)
	for _, tc := range []struct {
		method, token, body string
		id                  uint
		want                int
	}{
		{http.MethodGet, "", "", f.subscription, http.StatusUnauthorized},
		{http.MethodGet, f.base.token, "", 999999, http.StatusNotFound},
		{http.MethodPut, f.base.token, `{"enabled":true,"plan_sku_id":1,"base_price_minor":1}`, f.subscription, http.StatusBadRequest},
		{http.MethodPut, f.base.token, `{"enabled":true,"plan_sku_id":1,"user_id":2}`, f.subscription, http.StatusBadRequest},
		{http.MethodPut, f.base.token, `{"enabled":true,"plan_sku_id":1,"quote_fingerprint":"forged"}`, f.subscription, http.StatusBadRequest},
	} {
		out := httptest.NewRecorder()
		f.base.h.AutomaticRenewalHandler(out, announcementRequest(tc.method, fmt.Sprintf("/api/v1/subscriptions/%d/automatic-renewal", tc.id), tc.token, tc.body))
		if out.Code != tc.want {
			t.Fatalf("%s returned %d, want%d", tc.method, out.Code, tc.want)
		}
	}
	var count int64
	if err := f.db.Model(&model.SubscriptionAutomaticRenewal{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("invalid HTTP input created debit consent", count, err)
	}
}

func TestMySQLAutomaticRenewalVersusManualVerifiedCash(t *testing.T) {
	f := expiredRecoveryFixture(t, newMySQLOrderFixture(t))
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
	facts := commerce.PaymentFacts{Repository: commercestore.PaymentFacts{DB: f.db, Issuer: zeroadapter.Issuer{Cipher: f.base.h.credentialCipher, Mieru: f.base.h.zeroMieruAccess}, Now: func() time.Time { return *f.at }}}
	authority := commerce.PaymentFactAuthority{AccountID: 1, Provider: "sandbox-auto-race", RequireAdministrator: true}
	fact := commerce.PaymentFact{OrderID: pending.ID, ProviderEventID: "manual-versus-auto", ProviderTradeNo: "manual-versus-auto", Status: "paid", AmountMinor: 12000, Currency: "CNY", OccurredAt: *f.at}
	start := make(chan struct{})
	done := make(chan error, 2)
	go func() { <-start; _, err := service.Process(t.Context(), 20); done <- err }()
	go func() { <-start; _, err := facts.Record(t.Context(), authority, fact); done <- err }()
	close(start)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal("concurrent renewal failed", err)
		}
	}
	if _, err := facts.Record(t.Context(), authority, fact); err != nil {
		t.Fatal("payment replay failed", err)
	}
	var sub model.Subscription
	var wallet model.WalletAccount
	if err := f.db.First(&sub, f.subscription).Error; err != nil || sub.EntitlementEpoch != 1 || !sub.EndAt.Equal(f.at.AddDate(1, 0, 0)) {
		t.Fatal("manual and automatic payment both granted", sub, err)
	}
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 18000 || wallet.ReservedMinor != 0 {
		t.Fatal("race lost or duplicated verified cash/wallet money", wallet, err)
	}
	var paid int64
	if err := f.db.Model(&model.Order{}).Where("subscription_id = ? AND order_type = ? AND status = ?", f.subscription, "renewal", "paid").Count(&paid).Error; err != nil || paid != 1 {
		t.Fatal("race produced duplicate fulfilled renewals", paid, err)
	}
}

func TestAutomaticRenewalKeepsBasePriceAndRequiresConsentForNewTerms(t *testing.T) {
	assertAutomaticRenewalTerms(t, newOrderFixture(t))
}
func TestMySQLAutomaticRenewalKeepsBasePriceAndRequiresConsentForNewTerms(t *testing.T) {
	assertAutomaticRenewalTerms(t, newMySQLOrderFixture(t))
}
func assertAutomaticRenewalTerms(t *testing.T, base orderFixture) {
	f := expiredRecoveryFixture(t, base)
	setRecoveryPolicy(t, f, 14)
	service := automaticRenewalsFor(f)
	enableAutomaticRenewal(t, f, service)
	if err := f.db.Model(&model.PlanSKU{}).Where("id = ?", base.skuRecord.ID).Update("price_cents", 24000).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := service.Process(t.Context(), 20); err != nil || n != 1 {
		t.Fatal("catalog increase changed purchased renewal base", n, err)
	}
	var wallet model.WalletAccount
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 6000 {
		t.Fatal("catalog increase charged current list price", wallet, err)
	}
	var sub model.Subscription
	if err := f.db.First(&sub, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	*f.at = sub.EndAt.Add(time.Hour)
	if err := f.db.Model(&model.Plan{}).Where("id = ?", sub.PlanID).Update("traffic_bytes", 200).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := service.Process(t.Context(), 20); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	view, err := service.Read(t.Context(), 1, f.subscription, 0)
	if err != nil || view.Enabled || view.State != "conditions_changed" {
		t.Fatal("changed entitlement terms silently kept debit consent", view, err)
	}
	wallet = model.WalletAccount{}
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 6000 || wallet.ReservedMinor != 0 {
		t.Fatal("unconfirmed changed terms reserved money", wallet, err)
	}
}

func TestAutomaticRenewalUnknownHistoricalDiscountNeverRevertsToFullPrice(t *testing.T) {
	base := newOrderFixture(t)
	order, err := base.h.services.OrderCreation.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: base.skuRecord.ID})
	if err != nil {
		t.Fatal(err)
	}
	// Emulate a trusted pre-upgrade discounted pending quote. The actual
	// verified receipt is80, not a fabricated full-price100 payment. This does
	// not claim that a coupon plugin or its renewal agreement exists.
	if err := base.h.db.Model(&model.Order{}).Where("id = ?", order.ID).Updates(map[string]any{"payable_amount": 80, "discount_amount": 20}).Error; err != nil {
		t.Fatal(err)
	}
	facts := commerce.PaymentFacts{Repository: commercestore.PaymentFacts{DB: base.h.db, Issuer: zeroadapter.Issuer{Cipher: base.h.credentialCipher, Mieru: base.h.zeroMieruAccess}}}
	receipt, err := facts.Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: "sandbox-legacy-discount", RequireAdministrator: true}, commerce.PaymentFact{OrderID: order.ID, ProviderEventID: "actual-discounted-cash", ProviderTradeNo: "actual-discounted-cash", Status: "paid", AmountMinor: 80, Currency: "CNY", OccurredAt: time.Now().UTC()})
	if err != nil || !receipt.Fulfilled {
		t.Fatal(receipt, err)
	}
	var paid model.Order
	if err := base.h.db.First(&paid, order.ID).Error; err != nil || paid.PaidAmount != 80 {
		t.Fatal("discount fixture did not preserve actual paid source", paid.PaidAmount, err)
	}
	view, err := base.h.services.AutomaticRenewals(base.h.credentialCipher, base.h.zeroMieruAccess).Read(t.Context(), 1, paid.SubscriptionID, base.skuRecord.ID)
	if err != nil || view.Enabled || view.Offer != nil || !strings.Contains(view.Message, "优惠") {
		t.Fatal("unknown discount became full-price automatic debit offer", view, err)
	}
}

func TestMySQLAutomaticRenewalVersusUnsubscribe(t *testing.T) {
	f := expiredRecoveryFixture(t, newMySQLOrderFixture(t))
	setRecoveryPolicy(t, f, 14)
	service := automaticRenewalsFor(f)
	enableAutomaticRenewal(t, f, service)
	clock := func() time.Time { return *f.at }
	values := commerce.SubscriptionValues{Repository: commercestore.SubscriptionValues{DB: f.db, Now: clock}}
	quote, err := values.Read(t.Context(), 1, f.subscription)
	if err != nil {
		t.Fatal(err)
	}
	termination := commerce.SubscriptionTermination{Repository: commercestore.SubscriptionTermination{DB: f.db, Now: clock}}
	start := make(chan struct{})
	done := make(chan error, 2)
	go func() { <-start; _, err := service.Process(t.Context(), 20); done <- err }()
	go func() {
		<-start
		_, err := termination.Terminate(t.Context(), 1, f.subscription, commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "unsubscribe-versus-auto"})
		if errors.Is(err, commerce.ErrSubscriptionQuoteChanged) {
			err = nil // Current quote must be reconfirmed after a winning renewal.
		}
		done <- err
	}()
	close(start)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal("unsubscribe/automatic renewal competition failed", err)
		}
	}
	var sub model.Subscription
	var wallet model.WalletAccount
	var agreement model.SubscriptionAutomaticRenewal
	if err := f.db.First(&sub, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&agreement, "subscription_id = ?", f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil {
		t.Fatal(err)
	}
	switch sub.Status {
	case "canceled":
		if agreement.Enabled || wallet.BalanceMinor != 18000 || sub.EntitlementEpoch != 0 {
			t.Fatal("unsubscribe winner retained automatic authority or charged", agreement, wallet.BalanceMinor, sub.EntitlementEpoch)
		}
	case "active":
		if wallet.BalanceMinor != 6000 || sub.EntitlementEpoch != 1 {
			t.Fatal("renewal winner was refunded against a stale quote", wallet.BalanceMinor, sub.EntitlementEpoch)
		}
	default:
		t.Fatal("unexpected final subscription status", sub.Status)
	}
	if wallet.ReservedMinor != 0 {
		t.Fatal("race stranded a reservation", wallet.ReservedMinor)
	}
}

func TestAutomaticRenewalPaidWalletReplayDoesNotLockEntireHistory(t *testing.T) {
	assertAutomaticPaidReplayHistory(t, newOrderFixture(t))
}
func TestMySQLAutomaticRenewalPaidWalletReplayDoesNotLockEntireHistory(t *testing.T) {
	assertAutomaticPaidReplayHistory(t, newMySQLOrderFixture(t))
}
func assertAutomaticPaidReplayHistory(t *testing.T, base orderFixture) {
	f := expiredRecoveryFixture(t, base)
	setRecoveryPolicy(t, f, 14)
	service := automaticRenewalsFor(f)
	enableAutomaticRenewal(t, f, service)
	if _, err := service.Process(t.Context(), 20); err != nil {
		t.Fatal(err)
	}
	var agreement model.SubscriptionAutomaticRenewal
	if err := f.db.First(&agreement, "subscription_id = ?", f.subscription).Error; err != nil || agreement.LastOrderID == nil {
		t.Fatal(agreement.LastOrderID, err)
	}
	// Imported canceled checkouts add no paid value or cash. Their existence
	// must not turn an already committed wallet payment replay into a history
	// valuation/locking request exceeding the current1000-source bound.
	rows := make([]model.Order, 1001)
	for i := range rows {
		rows[i] = model.Order{UserID: 1, PlanID: base.planRecord.ID, PlanSKUID: base.skuRecord.ID, TradeNo: fmt.Sprintf("imported-canceled-history-%04d", i), OrderType: "renewal", TargetSubscriptionID: &f.subscription, Status: "canceled", Currency: "CNY", SubscriptionEndedAt: f.at}
	}
	if err := f.db.CreateInBatches(&rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		out, err := f.payments.Pay(t.Context(), 1, *agreement.LastOrderID)
		if err != nil || out.Status != "paid" || out.Fulfilled {
			t.Fatal("paid replay acquired unrelated historical sources", out, err)
		}
	}
	var wallet model.WalletAccount
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 6000 || wallet.ReservedMinor != 0 {
		t.Fatal("history-independent replay changed money", wallet.BalanceMinor, wallet.ReservedMinor, err)
	}
}
