package handler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestOperationDiscountCompatibleReaderPreservesActualValueAndRenewalBase(t *testing.T) {
	assertOperationDiscountReader(t, newOrderFixture(t))
}
func TestMySQLOperationDiscountCompatibleReaderPreservesActualValueAndRenewalBase(t *testing.T) {
	assertOperationDiscountReader(t, newMySQLBusinessOrderFixture(t))
}
func assertOperationDiscountReader(t *testing.T, base orderFixture) {
	t.Helper()
	f := newFullReplacementFixture(t, base, false, 0, 3)
	if err := f.db.Create(&model.PlanSKUOperation{PlanSKUID: f.target, Operation: "renew"}).Error; err != nil {
		t.Fatal(err)
	}
	// This is a future accepted-order fixture. It does not pretend that this
	// reader-only increment already admits a live native one-order checkout.
	if _, err := f.orders.Preview(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.target, TargetSubscriptionID: f.subscription, Discount: &commerce.OrderDiscountSelection{ProviderID: "compat.reader.quote", Selection: "one"}}); err == nil {
		t.Fatal("reader increment unexpectedly admitted a new one-order offer")
	}
	created := confirmedReplacement(t, f, "compatible-one-order-snapshot")
	var order model.Order
	if err := f.db.First(&order, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	var target commerce.SubscriptionChangeSnapshot
	if err := json.Unmarshal([]byte(order.ChangeSnapshot), &target); err != nil {
		t.Fatal(err)
	}
	if target.Residual != 9000 {
		t.Fatalf("unexpected real actual-paid residual %+v", target)
	}
	order.DiscountAmount = 200
	order.PayableAmount = 1800
	target.Applied = 1800
	target.Credit = 1800
	target.Surplus = 7200
	raw, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	order.ChangeSnapshot = string(raw)
	policy, err := commerce.DecodePurchaseSnapshot(order.PurchaseSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	in, err := commerce.NewOperationDiscountContext(commerce.Order(order), policy, "single-order")
	if err != nil {
		t.Fatal(err)
	}
	terms := `{"discount_minor":200}`
	termsHash, err := commerce.DiscountTermsHash(terms)
	if err != nil {
		t.Fatal(err)
	}
	offer := commerce.DiscountQuote{Provider: commerce.DiscountProviderIdentity{PluginID: "compat.reader.quote", SigningKeyFingerprint: strings.Repeat("a", 64), PackageDigest: strings.Repeat("b", 64), Generation: 1, ConfigRevision: 1}, Offer: commerce.DiscountOffer{Status: "available", InputFingerprint: in.InputFingerprint, Currency: "CNY", DiscountMinor: 200, ExpiresAt: f.at.Add(time.Hour), Agreement: &commerce.DiscountAgreement{ID: "single-order", Version: "1", Scope: commerce.OperationDiscountScope, TermsJSON: terms, TermsHash: termsHash}}}
	policy.Discount = &commerce.PurchaseDiscountSnapshot{Version: 3, Context: in, Quote: offer, FinalPriceMinor: 1800, AcceptedAt: *f.at}
	if err := commerce.ValidatePurchaseDiscount(commerce.Order(order), policy); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	order.PurchaseSnapshot = string(raw)
	if err := f.db.Model(&order).Updates(map[string]any{"discount_amount": 200, "payable_amount": 1800, "change_snapshot": order.ChangeSnapshot, "purchase_snapshot": order.PurchaseSnapshot}).Error; err != nil {
		t.Fatal(err)
	}
	funding, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true})
	if err != nil {
		t.Fatal(err)
	}
	if funding.EntitlementMinor != 1800 || funding.CashMinor != 0 || funding.ReservedMinor != 0 {
		t.Fatalf("future accepted funding %+v", funding)
	}
	// A corrupted accepted target cannot be paid. Restore the exact fixture only
	// after proving that no settlement or entitlement change was committed.
	good := order.PurchaseSnapshot
	bad := policy
	copyDiscount := *policy.Discount
	bad.Discount = &copyDiscount
	bad.Discount.Context.TargetSubscriptionID++
	raw, _ = json.Marshal(bad)
	if err := f.db.Model(&order).Update("purchase_snapshot", string(raw)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Pay(t.Context(), 1, order.ID); err == nil {
		t.Fatal("changed accepted target was paid")
	}
	var count int64
	if err := f.db.Model(&model.SubscriptionValueSettlement{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("invalid target settled %d %v", count, err)
	}
	var untouched model.Subscription
	if err := f.db.First(&untouched, f.subscription).Error; err != nil || untouched.PlanSKUID == f.target {
		t.Fatalf("invalid target changed rights %+v %v", untouched, err)
	}
	if err := f.db.Model(&order).Update("purchase_snapshot", good).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := f.payments.Pay(t.Context(), 1, order.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.First(&order, order.ID).Error; err != nil || order.PaidAmount != 1800 || order.AmountCents != 2000 || order.DiscountAmount != 200 {
		t.Fatalf("base/actual/reduction lost %+v %v", order, err)
	}
	var wallet model.WalletAccount
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 25200 || wallet.ReservedMinor != 0 {
		t.Fatalf("discount became cash or repeated surplus %+v %v", wallet, err)
	}
	clock := func() time.Time { return *f.at }
	values := commerce.SubscriptionValues{Repository: commercestore.SubscriptionValues{DB: f.db, Now: clock}}
	value, err := values.Read(t.Context(), 1, f.subscription)
	if err != nil || value.TotalMinor != 1800 {
		t.Fatalf("new rights not valued by actual spend %+v %v", value, err)
	}
	if err := f.db.Model(&model.PlanSKU{}).Where("id = ?", f.target).Update("price_cents", 5000).Error; err != nil {
		t.Fatal(err)
	}
	request := commerce.OrderCreateRequest{PlanSKUID: f.target, TargetSubscriptionID: f.subscription, IdempotencyKey: "reader-compatible-renewal"}
	renewal, err := f.orders.Preview(t.Context(), 1, request)
	if err != nil || renewal.BasePriceMinor != 2000 || renewal.PayableAmount != 2000 || renewal.DiscountAmount != 0 || renewal.Discount != nil {
		t.Fatalf("one-order offer replaced base or repeated itself %+v %v", renewal, err)
	}
	request.QuoteFingerprint = renewal.QuoteFingerprint
	renewed, err := f.orders.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, renewed.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Pay(t.Context(), 1, renewed.ID); err != nil {
		t.Fatal(err)
	}
	value, err = values.Read(t.Context(), 1, f.subscription)
	if err != nil || value.TotalMinor != 3800 {
		t.Fatalf("current/future actual values %+v %v", value, err)
	}
	termination := commerce.SubscriptionTermination{Repository: commercestore.SubscriptionTermination{DB: f.db, Now: clock}}
	receipt, err := termination.Terminate(t.Context(), 1, f.subscription, commerce.SubscriptionTerminationRequest{QuoteFingerprint: value.Fingerprint, IdempotencyKey: "reader-compatible-whole-stop"})
	if err != nil || receipt.AmountMinor != 3800 {
		t.Fatalf("actual money termination %+v %v", receipt, err)
	}
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 27000 || wallet.ReservedMinor != 0 {
		t.Fatalf("coupon minted money or actual value lost %+v %v", wallet, err)
	}
}
