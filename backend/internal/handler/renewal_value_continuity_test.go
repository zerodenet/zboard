package handler

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type renewalValueFixture struct {
	f            orderFixture
	at           *time.Time
	orders       commerce.OrderCreation
	payments     commerce.WalletPayments
	values       commerce.SubscriptionValues
	terminate    commerce.SubscriptionTermination
	root         model.Order
	subscription uint
	monthly      uint
}

func newRenewalValueFixture(t *testing.T, f orderFixture) renewalValueFixture {
	t.Helper()
	at := time.Date(2025, 10, 19, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return at }
	db := f.h.db.Session(&gorm.Session{NowFunc: clock})
	if err := db.Model(&f.planRecord).Updates(map[string]any{"traffic_bytes": 100, "reset_policy": 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&f.skuRecord).Updates(map[string]any{"price_cents": 12000, "billing_unit": "year", "billing_value": 1, "renewal_effect": "extend_only"}).Error; err != nil {
		t.Fatal(err)
	}
	issuer := zeroadapter.Issuer{Cipher: f.h.credentialCipher, Mieru: f.h.zeroMieruAccess}
	fixture := renewalValueFixture{f: f, at: &at, orders: commerce.OrderCreation{Repository: commercestore.OrderCreation{DB: db, Now: clock}}, payments: commerce.WalletPayments{Repository: commercestore.WalletPayments{DB: db, Issuer: issuer, Now: clock}}, values: commerce.SubscriptionValues{Repository: commercestore.SubscriptionValues{DB: db, Now: clock}}, terminate: commerce.SubscriptionTermination{Repository: commercestore.SubscriptionTermination{DB: db, Now: clock}}}
	// Controlled-clock application simulation, including a trusted sandbox top-up.
	// This is not a live channel payment or production historical backfill.
	wallet := commerce.Wallet{Repository: commercestore.Wallet{DB: db}}
	topup, err := wallet.CreateTopUp(t.Context(), 1, commerce.TopUpRequest{AmountMinor: 30000, Currency: "CNY", IdempotencyKey: "renewal-value-historical-funding"})
	if err != nil {
		t.Fatal(err)
	}
	facts := commerce.PaymentFacts{Repository: commercestore.PaymentFacts{DB: db, Issuer: issuer, Now: clock}}
	if _, err := facts.Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: "test-renewal-value", RequireAdministrator: true}, commerce.PaymentFact{OrderID: topup.ID, ProviderEventID: "historical-funding", ProviderTradeNo: "historical-funding", Status: "paid", AmountMinor: 30000, Currency: "CNY", OccurredAt: at}); err != nil {
		t.Fatal(err)
	}
	root := fixture.create(t, f.skuRecord.ID, 0, "renewal-value-root")
	fixture.pay(t, root.ID)
	fact := readValueEvidence(t, f, root.ID)
	fixture.root = root
	fixture.subscription = fact.SubscriptionID
	at = time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	next := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	// Authoritative current quota reflects intervening resets with no prior use.
	if err := db.Model(&model.Subscription{}).Where("id = ?", fixture.subscription).Updates(map[string]any{"flow_used": 80, "flow_total": 100, "cycle_start_used": 0, "next_reset_at": next}).Error; err != nil {
		t.Fatal(err)
	}
	monthly := f.sku(t, f.planRecord.ID, 1000, "renew")
	fixture.monthly = monthly.ID
	return fixture
}
func (f renewalValueFixture) create(t *testing.T, sku, target uint, key string) model.Order {
	t.Helper()
	in := commerce.OrderCreateRequest{PlanSKUID: sku, TargetSubscriptionID: target, IdempotencyKey: key}
	quote, err := f.orders.Preview(t.Context(), 1, in)
	if err != nil {
		t.Fatal(err)
	}
	in.QuoteFingerprint = quote.QuoteFingerprint
	out, err := f.orders.Create(t.Context(), 1, in)
	if err != nil {
		t.Fatal(err)
	}
	return model.Order(out)
}
func (f renewalValueFixture) pay(t *testing.T, id uint) {
	t.Helper()
	if _, err := f.payments.Configure(t.Context(), 1, id, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Pay(t.Context(), 1, id); err != nil {
		t.Fatal(err)
	}
}
func TestApprovedRenewalValueContinuityAndImmediateUnsubscribe(t *testing.T) {
	for _, repeated := range []bool{false, true} {
		name := "single"
		if repeated {
			name = "repeated"
		}
		t.Run(name, func(t *testing.T) { assertRenewalValueContinuity(t, newOrderFixture(t), repeated) })
	}
}
func TestMySQLApprovedRenewalValueContinuityAndImmediateUnsubscribe(t *testing.T) {
	for _, repeated := range []bool{false, true} {
		name := "single"
		if repeated {
			name = "repeated"
		}
		t.Run(name, func(t *testing.T) { assertRenewalValueContinuity(t, newMySQLBusinessOrderFixture(t), repeated) })
	}
}
func assertRenewalValueContinuity(t *testing.T, base orderFixture, repeated bool) {
	f := newRenewalValueFixture(t, base)
	old, err := f.values.Read(t.Context(), 1, f.subscription)
	if err != nil || old.TotalMinor != 120 {
		t.Fatalf("old actual paid residual %+v %v", old, err)
	}
	one := f.create(t, f.monthly, f.subscription, "renewal-current-cycle-one")
	f.pay(t, one.ID)
	for i := 0; i < 2; i++ {
		if _, err := f.payments.Pay(t.Context(), 1, one.ID); err != nil {
			t.Fatal(err)
		}
	}
	quote, err := f.values.Read(t.Context(), 1, f.subscription)
	if err != nil || quote.TotalMinor != 1120 || quote.CurrentMinor != 539 || quote.FutureMinor != 581 {
		t.Fatalf("approved 11.20 real DB quote %+v %v", quote, err)
	}
	var sub model.Subscription
	if err := base.h.db.First(&sub, f.subscription).Error; err != nil || sub.FlowTotal != 100 || sub.FlowUsed != 80 || sub.CycleStartUsed != 0 || sub.NextResetAt == nil || !sub.NextResetAt.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("renewal restored/moved quota %+v %v", sub, err)
	}
	expected, entries := int64(1120), 6
	refunds := map[uint]int64{f.root.ID: 120, one.ID: 1000}
	if repeated {
		two := f.create(t, f.monthly, f.subscription, "renewal-current-cycle-two")
		f.pay(t, two.ID)
		quote, err = f.values.Read(t.Context(), 1, f.subscription)
		if err != nil || quote.TotalMinor != 2120 || quote.CurrentMinor != 539 || quote.FutureMinor != 1581 {
			t.Fatalf("repeated renewal value %+v %v", quote, err)
		}
		expected, entries = 2120, 8
		refunds[two.ID] = 1000
	}
	request := commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "unsubscribe-repeated-renewals"}
	for i := 0; i < 3; i++ {
		out, err := f.terminate.Terminate(t.Context(), 1, f.subscription, request)
		if err != nil || out.AmountMinor != expected {
			t.Fatalf("immediate refund %+v %v", out, err)
		}
	}
	walletMatches(t, base, 18120, 0, entries)
	for id, refund := range refunds {
		var budget model.OrderValueBudget
		if err := base.h.db.First(&budget, id).Error; err != nil || budget.RefundedMinor != refund || budget.ConvertedMinor != 0 || budget.RefundedMinor > budget.FundingMinor {
			t.Fatalf("cash source conservation %+v %v", budget, err)
		}
	}
}
func TestApprovedRenewalValueConcurrentPaymentAndUnsubscribe(t *testing.T) {
	assertRenewalValueRace(t, newOrderFixture(t))
}
func TestMySQLApprovedRenewalValueConcurrentPaymentAndUnsubscribe(t *testing.T) {
	assertRenewalValueRace(t, newMySQLBusinessOrderFixture(t))
}
func assertRenewalValueRace(t *testing.T, base orderFixture) {
	f := newRenewalValueFixture(t, base)
	one := f.create(t, f.monthly, f.subscription, "renewal-before-race")
	f.pay(t, one.ID)
	pending := f.create(t, f.monthly, f.subscription, "renewal-race-second")
	if _, err := f.payments.Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	quote, err := f.values.Read(t.Context(), 1, f.subscription)
	if err != nil || quote.TotalMinor != 1120 {
		t.Fatal(quote, err)
	}
	request := commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "unsubscribe-renewal-race"}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	var payErr, cancelErr error
	go func() { defer wg.Done(); <-start; _, payErr = f.payments.Pay(t.Context(), 1, pending.ID) }()
	go func() {
		defer wg.Done()
		<-start
		_, cancelErr = f.terminate.Terminate(t.Context(), 1, f.subscription, request)
	}()
	close(start)
	wg.Wait()
	if cancelErr != nil {
		if !errors.Is(cancelErr, commerce.ErrSubscriptionQuoteChanged) || payErr != nil {
			t.Fatalf("unexpected race %v %v", payErr, cancelErr)
		}
		fresh, err := f.values.Read(t.Context(), 1, f.subscription)
		if err != nil || fresh.TotalMinor != 2120 {
			t.Fatal(fresh, err)
		}
		if _, err := f.terminate.Terminate(t.Context(), 1, f.subscription, commerce.SubscriptionTerminationRequest{QuoteFingerprint: fresh.Fingerprint, IdempotencyKey: "unsubscribe-race-reconfirmed"}); err != nil {
			t.Fatal(err)
		}
	} else if payErr == nil {
		t.Fatal("both stale renewal and old refund quote committed")
	}
	walletMatches(t, base, 18120, 0, 8)
	var settlements int64
	if err := base.h.db.Model(&model.SubscriptionValueSettlement{}).Count(&settlements).Error; err != nil || settlements != 1 {
		t.Fatalf("duplicate concurrent settlement %d %v", settlements, err)
	}
}
