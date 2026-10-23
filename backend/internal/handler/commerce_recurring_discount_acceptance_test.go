package handler

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
)

func recurringNativeSetup(t *testing.T, f orderFixture) (*plugins.Manager, string, uint64, string) {
	t.Helper()
	priceForFunding(t, f, 12000)
	if err := f.h.db.Model(&f.planRecord).Update("is_renewable", true).Error; err != nil {
		t.Fatal(err)
	}
	attachOrderPublishEndpoint(t, f)
	// This helper credits the wallet through a core-authorized PaymentFact and
	// an existing payment attempt; it does not insert an artificial balance.
	fundTestWallet(t, f, 50000)
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := plugins.NewManager(f.h.db, f.h.credentialCipher, plugins.Options{Directory: t.TempDir(), TrustedPublishers: map[string]string{"quote.tests": base64.StdEncoding.EncodeToString(public)}}, "v0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	f.h.SetPluginManager(manager)
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
	signer := sha256.Sum256(public)
	return manager, installation.ID, installation.Generation, hex.EncodeToString(signer[:])
}

func recurringHTTP(t *testing.T, f orderFixture, preview bool, request commerce.OrderCreateRequest) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	path, handler := "/api/v1/orders", f.h.OrderCreateCommerceValidatedHandler
	if preview {
		path, handler = "/api/v1/orders/preview", f.h.OrderPreviewHandler
	}
	handler(response, announcementRequest(http.MethodPost, path, f.token, string(raw)))
	return response
}

func recurringPreview(t *testing.T, f orderFixture, request commerce.OrderCreateRequest) commerce.OrderPreview {
	t.Helper()
	response := recurringHTTP(t, f, true, request)
	var result struct{ Data commerce.OrderPreview }
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || result.Data.BasePriceMinor != 12000 || result.Data.DiscountAmount != 2000 || result.Data.PayableAmount != 10000 || result.Data.QuoteFingerprint == "" {
		t.Fatalf("recurring native HTTP preview: status=%d body=%s error=%v", response.Code, response.Body.String(), err)
	}
	return result.Data
}

func recurringCreate(t *testing.T, f orderFixture, request commerce.OrderCreateRequest) model.Order {
	t.Helper()
	response := recurringHTTP(t, f, false, request)
	var result struct{ Data commerce.Order }
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || result.Data.ID == 0 {
		t.Fatalf("recurring native HTTP create: status=%d body=%s error=%v", response.Code, response.Body.String(), err)
	}
	var order model.Order
	if err := f.h.db.First(&order, result.Data.ID).Error; err != nil {
		t.Fatal(err)
	}
	return order
}

func recurringWalletPay(t *testing.T, f orderFixture, id uint) (model.Order, commerce.PurchaseSnapshot) {
	t.Helper()
	service := fundingService(f)
	view, err := service.Configure(t.Context(), 1, id, commerce.FundingRequest{UseBalance: true})
	if err != nil || view.AmountMinor != 10000 || view.WalletMinor != 10000 || view.CashMinor != 0 {
		t.Fatalf("recurring wallet allocation: %+v %v", view, err)
	}
	paid, err := service.Pay(t.Context(), 1, id)
	if err != nil || !paid.Fulfilled || paid.Status != "paid" || paid.SubscriptionID == 0 {
		t.Fatalf("recurring trusted wallet fulfillment: %+v %v", paid, err)
	}
	var order model.Order
	if err := f.h.db.First(&order, id).Error; err != nil || order.Status != "paid" || order.Channel != "wallet" || order.Currency != "CNY" || order.AmountCents != 12000 || order.DiscountAmount != 2000 || order.PayableAmount != 10000 || order.PaidAmount != 10000 {
		t.Fatalf("recurring paid money invariant: id=%d error=%v", id, err)
	}
	snapshot, err := commerce.DecodePurchaseSnapshot(order.PurchaseSnapshot)
	if err != nil || commerce.ValidatePurchaseDiscount(commerce.Order(order), snapshot) != nil || snapshot.BasePriceMinor != 12000 || snapshot.Discount == nil || snapshot.Discount.Version != 2 || snapshot.Discount.FinalPriceMinor != 10000 || snapshot.Discount.Quote.Offer.DiscountMinor != 2000 || snapshot.Discount.Quote.Offer.Agreement.Scope != "recurring" {
		t.Fatalf("invalid V2 recurring purchase evidence: id=%d error=%v", id, err)
	}
	var value commerce.EntitlementValueSnapshot
	if err := json.Unmarshal([]byte(order.EntitlementValueSnapshot), &value); err != nil || value.Version != 1 || !value.PaymentVerified || value.LegacyTransferUnproven || value.OrderID != id || value.SubscriptionID != order.SubscriptionID || value.Currency != "CNY" || value.ActualPaidMinor != 10000 || !value.After.EndAt.After(value.After.StartAt) {
		t.Fatalf("missing actual wallet/entitlement evidence: id=%d error=%v", id, err)
	}
	return order, snapshot
}

func TestCommerceRecurringDiscountNativeHTTPWalletAndManualRenewals(t *testing.T) {
	assertCommerceRecurringDiscountNativeHTTPWalletAndManualRenewals(t, newOrderFixture(t))
}

func TestMySQLCommerceRecurringDiscountNativeHTTPWalletAndManualRenewals(t *testing.T) {
	assertCommerceRecurringDiscountNativeHTTPWalletAndManualRenewals(t, newMySQLBusinessOrderFixture(t))
}

func assertCommerceRecurringDiscountNativeHTTPWalletAndManualRenewals(t *testing.T, f orderFixture) {
	t.Helper()
	manager, provider, generation, signer := recurringNativeSetup(t, f)
	f.token, _, _ = f.h.issueToken(authClaims{UserID: 1, Email: "reader@example.test"})
	request := commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, IdempotencyKey: "native-recurring-new", Discount: &commerce.OrderDiscountSelection{ProviderID: provider, Selection: "recurring"}}
	request.QuoteFingerprint = recurringPreview(t, f, request).QuoteFingerprint
	created := recurringCreate(t, f, request)
	root, rootPolicy := recurringWalletPay(t, f, created.ID)
	if rootPolicy.Discount.Source != nil || rootPolicy.Discount.Quote.Provider.SigningKeyFingerprint != signer {
		t.Fatal("new recurring evidence invented a prior source or lost its signer")
	}
	walletMatches(t, f, 40000, 0, 3)
	var sub model.Subscription
	if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	priceForFunding(t, f, 20000)
	previous := root
	var lastRequest commerce.OrderCreateRequest
	for i := 1; i <= 2; i++ {
		// Browser omission must inherit the paid agreement, never quote full price.
		request = commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, TargetSubscriptionID: sub.ID, IdempotencyKey: fmt.Sprintf("native-recurring-renew-%d", i)}
		request.QuoteFingerprint = recurringPreview(t, f, request).QuoteFingerprint
		created = recurringCreate(t, f, request)
		paid, policy := recurringWalletPay(t, f, created.ID)
		source := policy.Discount.Source
		if source == nil || commerce.ValidateDiscountSource(source) != nil || source.SubscriptionID != sub.ID || source.RootOrderID != root.ID || source.BaseOrderID != root.ID || source.AgreementOrderID != previous.ID || source.ProviderID != provider || source.SigningKeyFingerprint != signer || source.Selection != "recurring" || source.Agreement != *rootPolicy.Discount.Quote.Offer.Agreement || *policy.Discount.Context.Agreement != source.Agreement || *policy.Discount.Quote.Offer.Agreement != source.Agreement || policy.RenewalTarget == nil || !policy.RenewalTarget.EndAt.Equal(sub.EndAt) {
			t.Fatalf("renewal %d lost its strict paid source chain", i)
		}
		expectedEnd, err := entitlements.AddBillingPeriod(sub.EndAt, paid.BillingUnit, paid.BillingValue)
		if err != nil {
			t.Fatal(err)
		}
		var after model.Subscription
		if err := f.h.db.First(&after, sub.ID).Error; err != nil || !after.EndAt.Equal(expectedEnd) || !after.StartAt.Equal(sub.StartAt) || after.ResetPolicy != sub.ResetPolicy || after.FlowTotal != sub.FlowTotal || after.FlowUsed != sub.FlowUsed {
			t.Fatalf("renewal %d did not extend exactly one purchased term: %v", i, err)
		}
		walletMatches(t, f, 40000-int64(i)*10000, 0, 3+2*i)
		sub, previous, lastRequest = after, paid, request
	}
	if _, err := manager.Action(t.Context(), provider, "disable", "admin", generation, false, ""); err != nil {
		t.Fatal(err)
	}
	var beforeOrders, beforeFunding int64
	if err := f.h.db.Model(&model.Order{}).Count(&beforeOrders).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.OrderFunding{}).Count(&beforeFunding).Error; err != nil {
		t.Fatal(err)
	}
	blocked := commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, TargetSubscriptionID: sub.ID, IdempotencyKey: "native-recurring-disabled", QuoteFingerprint: lastRequest.QuoteFingerprint}
	for _, preview := range []bool{true, false} {
		response := recurringHTTP(t, f, preview, blocked)
		if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "discount_unavailable") {
			t.Fatalf("omitted recurring selection bypassed disabled provider: preview=%v status=%d body=%s", preview, response.Code, response.Body.String())
		}
	}
	replayed := recurringCreate(t, f, lastRequest)
	if replayed.ID != previous.ID || replayed.PurchaseSnapshot != previous.PurchaseSnapshot || replayed.EntitlementValueSnapshot != previous.EntitlementValueSnapshot {
		t.Fatal("same-key paid renewal recovery changed source evidence or created a new order")
	}
	var afterOrders, afterFunding int64
	if err := f.h.db.Model(&model.Order{}).Count(&afterOrders).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.OrderFunding{}).Count(&afterFunding).Error; err != nil || afterOrders != beforeOrders || afterFunding != beforeFunding {
		t.Fatalf("disabled provider/replay left orders or funding: orders=%d/%d funding=%d/%d error=%v", afterOrders, beforeOrders, afterFunding, beforeFunding, err)
	}
	walletMatches(t, f, 20000, 0, 7)
}
