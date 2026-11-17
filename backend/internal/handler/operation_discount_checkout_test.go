package handler

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"gorm.io/gorm"
)

func TestOperationDiscountNativeHTTPCheckoutAndWallet(t *testing.T) {
	assertOperationDiscountNativeCheckout(t, newOrderFixture(t))
}
func TestMySQLOperationDiscountNativeHTTPCheckoutAndWallet(t *testing.T) {
	assertOperationDiscountNativeCheckout(t, newMySQLBusinessOrderFixture(t))
}

// A signed local native fixture exercises actual discovery, RPC, HTTP
// quote/create and core wallet fulfillment for all three target operations.
// This is not an external limited-use coupon or remote-node acceptance test.
func assertOperationDiscountNativeCheckout(t *testing.T, base orderFixture) {
	t.Helper()
	f := newFullReplacementFixture(t, base, false, 0, 3)
	if err := f.db.Create(&model.PlanSKUOperation{PlanSKUID: f.target, Operation: "renew"}).Error; err != nil {
		t.Fatal(err)
	}
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	manager, err := plugins.NewManager(f.db, base.h.credentialCipher, plugins.Options{Directory: t.TempDir(), TrustedPublishers: map[string]string{"quote.tests": base64.StdEncoding.EncodeToString(public)}}, "v0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	base.h.SetPluginManager(manager)
	installation, err := manager.Import(discountNativePackage(t, public, private), "admin")
	if err != nil {
		t.Fatal(err)
	}
	installation, err = manager.Action(t.Context(), installation.ID, "enable", "admin", installation.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SaveConfig(t.Context(), installation.ID, "admin", 0, []byte(`{"enabled":true}`)); err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return *f.at }
	f.orders = commerce.OrderCreation{Repository: commercestore.OrderCreation{DB: f.db, Now: clock, Discounts: application.CommerceDiscountProvider{Native: manager}}}
	base.h.services.OrderCreation = f.orders
	if err := f.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	base.token, _, _ = base.h.issueToken(authClaims{UserID: 1, Email: "reader@example.test"})
	discovery := httptest.NewRecorder()
	base.h.CommerceDiscountProvidersHandler(discovery, announcementRequest(http.MethodGet, "/api/v1/commerce/discount-providers", base.token, ""))
	var choices struct {
		Data []commerce.DiscountProviderChoice
	}
	if json.Unmarshal(discovery.Body.Bytes(), &choices) != nil || discovery.Code != 200 || len(choices.Data) != 1 || len(choices.Data[0].Operations) != 4 || choices.Data[0].AgreementScope != "quote_required" {
		t.Fatal("operation discovery", discovery.Code, discovery.Body.String())
	}
	request := commerce.OrderCreateRequest{PlanSKUID: f.target, TargetSubscriptionID: f.subscription, IdempotencyKey: "native-one-order-change", Discount: &commerce.OrderDiscountSelection{ProviderID: installation.ID, Selection: "one-order"}}
	call := func(handler http.HandlerFunc, path string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(request)
		response := httptest.NewRecorder()
		handler(response, announcementRequest(http.MethodPost, path, base.token, string(raw)))
		return response
	}
	preview := func() commerce.OrderPreview {
		response := call(base.h.OrderPreviewHandler, "/api/v1/orders/preview")
		var out struct{ Data commerce.OrderPreview }
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &out) != nil {
			t.Fatal("operation quote", response.Code, response.Body.String())
		}
		return out.Data
	}
	create := func() commerce.Order {
		response := call(base.h.OrderCreateCommerceValidatedHandler, "/api/v1/orders")
		var out struct{ Data commerce.Order }
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &out) != nil || out.Data.ID == 0 {
			t.Fatal("operation create", response.Code, response.Body.String())
		}
		return out.Data
	}
	for _, selection := range []string{"", "recurring"} {
		request.Discount.Selection = selection
		response := call(base.h.OrderPreviewHandler, "/api/v1/orders/preview")
		if response.Code != 400 && response.Code != 502 {
			t.Fatal("non-order-only scope accepted", response.Code, response.Body.String())
		}
	}
	request.Discount.Selection = "one-order"
	q := preview()
	if q.OrderType != "upgrade" || q.ChangeMode != "full_term" || q.BasePriceMinor != 2000 || q.DiscountAmount != 200 || q.PayableAmount != 1800 || q.SettlementAmount != 9000 || q.CreditAmount != 1800 || q.SurplusAmount != 7200 || q.AdditionalAmount != 0 || q.Discount.Offer.Agreement.Scope != commerce.OperationDiscountScope {
		t.Fatalf("discounted replacement allocation %+v", q)
	}
	if response := call(base.h.OrderCreateCommerceValidatedHandler, "/api/v1/orders"); response.Code != 400 {
		t.Fatal("unconfirmed one-order offer accepted", response.Code, response.Body.String())
	}
	request.QuoteFingerprint = q.QuoteFingerprint
	if _, err := manager.SaveConfig(t.Context(), installation.ID, "admin", 1, []byte(`{"enabled":true,"revision":2}`)); err != nil {
		t.Fatal(err)
	}
	if response := call(base.h.OrderCreateCommerceValidatedHandler, "/api/v1/orders"); response.Code != 400 {
		t.Fatal("changed provider config reused approval", response.Code, response.Body.String())
	}
	request.QuoteFingerprint = ""
	q = preview()
	request.QuoteFingerprint = q.QuoteFingerprint
	created := create()
	policy, err := commerce.DecodePurchaseSnapshot(created.PurchaseSnapshot)
	if err == nil {
		t.Fatal("private acceptance snapshot leaked through HTTP")
	}
	var stored model.Order
	if err := f.db.First(&stored, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	policy, err = commerce.DecodePurchaseSnapshot(stored.PurchaseSnapshot)
	if err != nil || policy.Discount == nil || policy.Discount.Version != 3 || policy.Discount.Source != nil || policy.Discount.Quote.Provider.ConfigRevision != 2 || commerce.ValidatePurchaseDiscount(commerce.Order(stored), policy) != nil {
		t.Fatal("missing exact accepted snapshot", err)
	}
	installation, err = manager.Action(t.Context(), installation.ID, "disable", "admin", installation.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if recovered := create(); recovered.ID != created.ID {
		t.Fatal("lost response created a different order")
	}
	request.IdempotencyKey = "new-operation-stopped-provider"
	if response := call(base.h.OrderCreateCommerceValidatedHandler, "/api/v1/orders"); response.Code != 503 {
		t.Fatal("stopped provider fell back to price", response.Code, response.Body.String())
	}
	if _, err := f.payments.Configure(t.Context(), 1, created.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := f.payments.Pay(t.Context(), 1, created.ID); err != nil {
			t.Fatal(err)
		}
	}
	var wallet model.WalletAccount
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 25200 || wallet.ReservedMinor != 0 {
		t.Fatalf("one-order value minted/spent twice %+v %v", wallet, err)
	}
	if err := f.db.Model(&model.PlanSKU{}).Where("id = ?", f.target).Update("price_cents", 5000).Error; err != nil {
		t.Fatal(err)
	}
	renewalRequest := commerce.OrderCreateRequest{PlanSKUID: f.target, TargetSubscriptionID: f.subscription, IdempotencyKey: "native-one-order-base-renewal"}
	renewal, err := f.orders.Preview(t.Context(), 1, renewalRequest)
	if err != nil || renewal.BasePriceMinor != 2000 || renewal.Discount != nil || renewal.PayableAmount != 2000 {
		t.Fatal("one-order offer was inherited or replaced base", renewal, err)
	}
	renewalRequest.QuoteFingerprint = renewal.QuoteFingerprint
	renewed, err := f.orders.Create(t.Context(), 1, renewalRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, renewed.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Pay(t.Context(), 1, renewed.ID); err != nil {
		t.Fatal(err)
	}
	values := commerce.SubscriptionValues{Repository: commercestore.SubscriptionValues{DB: f.db, Now: clock}}
	value, err := values.Read(t.Context(), 1, f.subscription)
	if err != nil || value.TotalMinor != 3800 {
		t.Fatal("discount turned into refundable principal", value, err)
	}
	termination := commerce.SubscriptionTermination{Repository: commercestore.SubscriptionTermination{DB: f.db, Now: clock}}
	receipt, err := termination.Terminate(t.Context(), 1, f.subscription, commerce.SubscriptionTerminationRequest{QuoteFingerprint: value.Fingerprint, IdempotencyKey: "native-discount-whole-stop"})
	if err != nil || receipt.AmountMinor != 3800 {
		t.Fatal("whole remaining-value termination", receipt, err)
	}
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 27000 || wallet.ReservedMinor != 0 {
		t.Fatalf("coupon became refundable money %+v %v", wallet, err)
	}
	// A second independent subscription exercises reset/add-on writes while the
	// first remains terminal. Re-enable only this local fixture provider.
	installation, err = manager.Action(t.Context(), installation.ID, "enable", "admin", installation.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := f.orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: base.skuRecord.ID, IdempotencyKey: "native-operation-independent-root"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, fresh.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Pay(t.Context(), 1, fresh.ID); err != nil {
		t.Fatal(err)
	}
	var paid model.Order
	if err := f.db.First(&paid, fresh.ID).Error; err != nil {
		t.Fatal(err)
	}
	var before model.Subscription
	if err := f.db.First(&before, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		operation, kind string
		price, traffic  int64
	}{{"reset", "traffic_reset", 500, 0}, {"addon", "traffic_addon", 1000, 200}} {
		sku := model.PlanSKU{PlanID: base.planRecord.ID, Code: "native-" + item.operation, Name: item.operation, BillingMode: "one_time", EntitlementMode: item.kind, BillingUnit: "once", BillingValue: 1, PriceCents: item.price, TrafficBytes: item.traffic, Currency: "CNY", IsActive: true}
		if err := f.db.Create(&sku).Error; err != nil {
			t.Fatal(err)
		}
		if err := f.db.Create(&model.PlanSKUOperation{PlanSKUID: sku.ID, Operation: item.operation}).Error; err != nil {
			t.Fatal(err)
		}
		request = commerce.OrderCreateRequest{PlanSKUID: sku.ID, TargetSubscriptionID: paid.SubscriptionID, IdempotencyKey: "native-operation-" + item.operation, Discount: &commerce.OrderDiscountSelection{ProviderID: installation.ID, Selection: "one-order"}}
		q = preview()
		if q.BasePriceMinor != item.price || q.DiscountAmount != 200 || q.PayableAmount != item.price-200 || q.Discount.Offer.Agreement.Scope != commerce.OperationDiscountScope {
			t.Fatal("reset/add-on amount", q)
		}
		request.QuoteFingerprint = q.QuoteFingerprint
		created = create()
		if _, err := f.payments.Configure(t.Context(), 1, created.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if _, err := f.payments.Pay(t.Context(), 1, created.ID); err != nil {
				t.Fatal(err)
			}
		}
		stored = model.Order{}
		if err := f.db.First(&stored, created.ID).Error; err != nil || stored.PaidAmount != item.price-200 || stored.DiscountAmount != 200 {
			t.Fatal("actual operation spend", stored, err)
		}
		var after model.Subscription
		if err := f.db.First(&after, paid.SubscriptionID).Error; err != nil || !after.EndAt.Equal(before.EndAt) || after.NextResetAt == nil || before.NextResetAt == nil || !after.NextResetAt.Equal(*before.NextResetAt) || after.PlanSKUID != before.PlanSKUID {
			t.Fatal("reset/add-on moved paid period", after, err)
		}
		if item.operation == "reset" && after.FlowTotal-after.CycleStartUsed != before.ResetQuotaBytes {
			t.Fatal("reset stacked extra quota", after)
		}
		if item.operation == "addon" && after.FlowTotal-after.CycleStartUsed != before.ResetQuotaBytes+200 {
			t.Fatal("add-on duplicated/missed quota", after)
		}
	}
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 13900 || wallet.ReservedMinor != 0 {
		t.Fatalf("reset/add-on wallet charged incorrectly %+v %v", wallet, err)
	}
	if value, err := values.Read(t.Context(), 1, paid.SubscriptionID); err != nil || value.TotalMinor != 13100 {
		t.Fatalf("reset/add-on must retain actual paid value 13100: %+v %v", value, err)
	}
	// The synthetic quoter below is explicitly separate from native evidence. A
	// callback that writes through the same one-connection SQLite pool (or real
	// MySQL row locks) proves RPC evaluation owns no financial transaction.
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	calls := 0
	stub := operationCheckoutQuoter(func(ctx context.Context, provider string, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
		calls++
		if err := f.db.WithContext(ctx).Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).UpdateColumn("entitlement_epoch", gorm.Expr("entitlement_epoch + 1")).Error; err != nil {
			return commerce.DiscountQuote{}, err
		}
		terms := `{"discount_minor":200}`
		hash, _ := commerce.DiscountTermsHash(terms)
		return commerce.DiscountQuote{Provider: commerce.DiscountProviderIdentity{PluginID: provider, SigningKeyFingerprint: strings.Repeat("a", 64), PackageDigest: strings.Repeat("b", 64), Generation: 1, ConfigRevision: 1}, Offer: commerce.DiscountOffer{Status: "available", InputFingerprint: in.InputFingerprint, Currency: in.Currency, DiscountMinor: 200, ExpiresAt: clock().Add(time.Minute), Agreement: &commerce.DiscountAgreement{ID: "one", Version: "1", Scope: commerce.OperationDiscountScope, TermsJSON: terms, TermsHash: hash}}}, nil
	})
	guarded := commerce.OrderCreation{Repository: commercestore.OrderCreation{DB: f.db, Now: clock, Discounts: stub}}
	var ordersBefore, ordersAfter int64
	if err := f.db.Model(&model.Order{}).Count(&ordersBefore).Error; err != nil {
		t.Fatal(err)
	}
	_, err = guarded.Preview(ctx, 1, commerce.OrderCreateRequest{PlanSKUID: f.target, TargetSubscriptionID: paid.SubscriptionID, IdempotencyKey: "changed-during-one-order-quote", Discount: &commerce.OrderDiscountSelection{ProviderID: "synthetic.one-order"}})
	var invalid *commerce.ValidationError
	if !errors.As(err, &invalid) || calls != 1 {
		t.Fatal("changed target was accepted or provider ran under locks", err, calls)
	}
	if err := f.db.Model(&model.Order{}).Count(&ordersAfter).Error; err != nil || ordersBefore != ordersAfter {
		t.Fatal("source change committed a business order", ordersBefore, ordersAfter, err)
	}
	var stopped model.Subscription
	if err := f.db.First(&stopped, f.subscription).Error; err != nil || stopped.EndedAt == nil {
		t.Fatal("independent order revived terminated subscription", stopped, err)
	}
	value, err = values.Read(t.Context(), 1, paid.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err = termination.Terminate(t.Context(), 1, paid.SubscriptionID, commerce.SubscriptionTerminationRequest{QuoteFingerprint: value.Fingerprint, IdempotencyKey: "native-reset-pack-whole-stop"})
	if err != nil || receipt.AmountMinor != 13100 {
		t.Fatal("reset/add-on actual remaining principal was not terminable", receipt, err)
	}
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 27000 || wallet.ReservedMinor != 0 {
		t.Fatalf("reset/add-on discount minted or lost principal %+v %v", wallet, err)
	}
}

type operationCheckoutQuoter func(context.Context, string, commerce.DiscountQuoteContext) (commerce.DiscountQuote, error)

func (f operationCheckoutQuoter) Quote(ctx context.Context, provider string, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
	return f(ctx, provider, in)
}
