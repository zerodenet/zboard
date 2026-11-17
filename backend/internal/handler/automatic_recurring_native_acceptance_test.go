package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func automaticRecurringNativeHTTP(t *testing.T, f orderFixture, sub uint, method string, request commerce.AutomaticRenewalRequest) (*httptest.ResponseRecorder, commerce.AutomaticRenewalView) {
	t.Helper()
	path := fmt.Sprintf("/api/v1/subscriptions/%d/automatic-renewal", sub)
	raw := []byte{}
	if method == http.MethodGet {
		path += fmt.Sprintf("?plan_sku_id=%d", f.skuRecord.ID)
	} else {
		var err error
		raw, err = json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
	}
	response := httptest.NewRecorder()
	f.h.AutomaticRenewalHandler(response, announcementRequest(method, path, f.token, string(raw)))
	var result struct{ Data commerce.AutomaticRenewalView }
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal("native automatic renewal HTTP response", err)
	}
	return response, result.Data
}

func TestAutomaticRecurringSignedNativeHTTPWalletAndTwoAutomaticDebits(t *testing.T) {
	assertAutomaticRecurringSignedNativeHTTPWalletAndTwoAutomaticDebits(t, newOrderFixture(t))
}

func TestMySQLAutomaticRecurringSignedNativeHTTPWalletAndTwoAutomaticDebits(t *testing.T) {
	assertAutomaticRecurringSignedNativeHTTPWalletAndTwoAutomaticDebits(t, newMySQLBusinessOrderFixture(t))
}

func assertAutomaticRecurringSignedNativeHTTPWalletAndTwoAutomaticDebits(t *testing.T, f orderFixture) {
	t.Helper()
	// This installs the existing signed executable and wires the actual native
	// manager through application composition. No in-memory quoter is supplied.
	manager, provider, generation, signer := recurringNativeSetup(t, f)
	var err error
	f.token, _, err = f.h.issueToken(authClaims{UserID: 1, Email: "reader@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	request := commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, IdempotencyKey: "native-automatic-recurring-root", Discount: &commerce.OrderDiscountSelection{ProviderID: provider, Selection: "recurring"}}
	request.QuoteFingerprint = recurringPreview(t, f, request).QuoteFingerprint
	root, rootPolicy := recurringWalletPay(t, f, recurringCreate(t, f, request).ID)
	if rootPolicy.Discount.Quote.Provider.SigningKeyFingerprint != signer || rootPolicy.Discount.Quote.Provider.PluginID != provider || rootPolicy.Discount.Source != nil {
		t.Fatal("new recurring purchase did not retain signed native identity")
	}
	walletMatches(t, f, 40000, 0, 3)
	response, view := automaticRecurringNativeHTTP(t, f, root.SubscriptionID, http.MethodGet, commerce.AutomaticRenewalRequest{})
	if response.Code != http.StatusOK || view.Enabled || view.Offer == nil || view.Offer.AgreementVersion != 2 || view.Offer.BasePriceMinor != 12000 || view.Offer.DiscountAmountMinor != 2000 || view.Offer.PayableAmountMinor != 10000 || view.Offer.Discount == nil || view.Offer.Discount.Provider.SigningKeyFingerprint != signer || view.Offer.Discount.Offer.Agreement.Scope != "recurring" {
		t.Fatalf("signed native automatic offer: status=%d body=%s", response.Code, response.Body.String())
	}
	response, enabled := automaticRecurringNativeHTTP(t, f, root.SubscriptionID, http.MethodPut, commerce.AutomaticRenewalRequest{Enabled: true, PlanSKUID: f.skuRecord.ID, QuoteFingerprint: view.Offer.QuoteFingerprint, ExpectedRevision: view.Revision})
	if response.Code != http.StatusOK || !enabled.Enabled || enabled.Revision != view.Revision+1 {
		t.Fatalf("explicit HTTP automatic approval: status=%d body=%s", response.Code, response.Body.String())
	}
	var approved model.SubscriptionAutomaticRenewal
	if err := f.h.db.First(&approved, "subscription_id = ?", root.SubscriptionID).Error; err != nil || approved.AgreementVersion != 2 || approved.ApprovalSnapshot == "" || approved.RootOrderID != root.ID {
		t.Fatal("missing durable recurring approval", err)
	}
	// Only scheduler/target dates are compressed to make the real worker due.
	// Native RPC and expiry use the real clock; paid source snapshots, payment
	// facts, approval revision/digest/bytes and wallet facts are never rewritten.
	// This proves two debit executions, not passage of two natural months.
	makeDue := func() {
		t.Helper()
		due := time.Now().UTC().Add(-time.Second)
		if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", root.SubscriptionID).Update("end_at", due).Error; err != nil {
			t.Fatal(err)
		}
		if err := f.h.db.Model(&model.SubscriptionAutomaticRenewal{}).Where("subscription_id = ?", root.SubscriptionID).Update("next_attempt_at", due).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := f.h.services.AutomaticRenewals(f.h.credentialCipher, f.h.zeroMieruAccess)
	priceForFunding(t, f, 20000)
	previous := root
	for i := 1; i <= 2; i++ {
		makeDue()
		if n, err := service.Process(t.Context(), 20); err != nil || n != 1 {
			t.Fatal("actual native automatic debit", i, n, err)
		}
		if n, err := service.Process(t.Context(), 20); err != nil || n != 0 {
			t.Fatal("automatic debit replay charged the same term", n, err)
		}
		var current model.SubscriptionAutomaticRenewal
		if err := f.h.db.First(&current, "subscription_id = ?", root.SubscriptionID).Error; err != nil || !current.Enabled || current.State != "renewed" || current.LastOrderID == nil || current.Revision != approved.Revision || current.AgreementVersion != 2 || current.AgreementDigest != approved.AgreementDigest || current.ApprovalSnapshot != approved.ApprovalSnapshot {
			t.Fatal("native automatic debit rewrote stable approval", i, current.State, err)
		}
		var paid model.Order
		if err := f.h.db.First(&paid, *current.LastOrderID).Error; err != nil || paid.Status != "paid" || paid.Channel != "wallet" || paid.AmountCents != 12000 || paid.DiscountAmount != 2000 || paid.PayableAmount != 10000 || paid.PaidAmount != 10000 || paid.SubscriptionID != root.SubscriptionID {
			t.Fatal("native automatic debit monetary facts", i, err)
		}
		policy, err := commerce.DecodePurchaseSnapshot(paid.PurchaseSnapshot)
		if err != nil || commerce.ValidatePurchaseDiscount(commerce.Order(paid), policy) != nil || policy.Discount.Version != 2 || policy.Discount.Source == nil || policy.Discount.Source.RootOrderID != root.ID || policy.Discount.Source.BaseOrderID != root.ID || policy.Discount.Source.AgreementOrderID != previous.ID || policy.Discount.Quote.Provider.SigningKeyFingerprint != signer || *policy.Discount.Quote.Offer.Agreement != *rootPolicy.Discount.Quote.Offer.Agreement {
			t.Fatal("native automatic debit lost its signed paid series", i, err)
		}
		var value commerce.EntitlementValueSnapshot
		if err := json.Unmarshal([]byte(paid.EntitlementValueSnapshot), &value); err != nil || !value.PaymentVerified || value.LegacyTransferUnproven || value.ActualPaidMinor != 10000 || value.OrderID != paid.ID || value.SubscriptionID != root.SubscriptionID || !value.After.EndAt.After(value.After.StartAt) {
			t.Fatal("native automatic debit lacked trusted wallet entitlement evidence", i, err)
		}
		walletMatches(t, f, 40000-int64(i)*10000, 0, 3+i*2)
		previous = paid
	}
	makeDue()
	if _, err := manager.Action(t.Context(), provider, "disable", "admin", generation, false, ""); err != nil {
		t.Fatal(err)
	}
	var orderCount, fundingCount int64
	if err := f.h.db.Model(&model.Order{}).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.OrderFunding{}).Count(&fundingCount).Error; err != nil {
		t.Fatal(err)
	}
	response, unavailable := automaticRecurringNativeHTTP(t, f, root.SubscriptionID, http.MethodGet, commerce.AutomaticRenewalRequest{})
	if response.Code != http.StatusOK || !unavailable.Enabled || unavailable.Revision != approved.Revision || unavailable.Offer != nil || unavailable.Message == "" {
		t.Fatalf("stopped native provider hid revocable consent: status=%d body=%s", response.Code, response.Body.String())
	}
	response, _ = automaticRecurringNativeHTTP(t, f, root.SubscriptionID, http.MethodPut, commerce.AutomaticRenewalRequest{Enabled: true, PlanSKUID: f.skuRecord.ID, QuoteFingerprint: view.Offer.QuoteFingerprint, ExpectedRevision: unavailable.Revision})
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "discount_unavailable") {
		t.Fatalf("stopped provider accepted an automatic approval: status=%d body=%s", response.Code, response.Body.String())
	}
	if n, err := service.Process(t.Context(), 20); err != nil || n != 1 {
		t.Fatal("stopped provider did not persist a non-charge attempt", n, err)
	}
	var paused model.SubscriptionAutomaticRenewal
	if err := f.h.db.First(&paused, "subscription_id = ?", root.SubscriptionID).Error; err != nil || !paused.Enabled || paused.State != "agreement_unavailable" || paused.LastOrderID == nil || *paused.LastOrderID != previous.ID || paused.Revision != approved.Revision || paused.ApprovalSnapshot != approved.ApprovalSnapshot || paused.AgreementDigest != approved.AgreementDigest {
		t.Fatal("stopped provider changed approval or charged full price", paused.State, err)
	}
	var afterOrders, afterFunding int64
	if err := f.h.db.Model(&model.Order{}).Count(&afterOrders).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.OrderFunding{}).Count(&afterFunding).Error; err != nil || afterOrders != orderCount || afterFunding != fundingCount {
		t.Fatal("stopped provider created an order or reservation", err)
	}
	walletMatches(t, f, 20000, 0, 7)
	response, disabled := automaticRecurringNativeHTTP(t, f, root.SubscriptionID, http.MethodPut, commerce.AutomaticRenewalRequest{ExpectedRevision: paused.Revision})
	if response.Code != http.StatusOK || disabled.Enabled || disabled.Revision != paused.Revision+1 || disabled.State != "disabled" {
		t.Fatalf("HTTP withdrawal required stopped provider: status=%d body=%s", response.Code, response.Body.String())
	}
	if n, err := service.Process(t.Context(), 20); err != nil || n != 0 {
		t.Fatal("withdrawn consent remained due", n, err)
	}
	walletMatches(t, f, 20000, 0, 7)
}
