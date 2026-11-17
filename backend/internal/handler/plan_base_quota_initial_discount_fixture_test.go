package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
)

// Only the external quote is simulated. Acceptance, the persisted purchase
// agreement, payment, wallet debits and entitlement capture use real core paths.
// This does not assert external provider transport or production acceptance.
type planBaseQuotaInitialQuoter struct{}

func (planBaseQuotaInitialQuoter) Quote(ctx context.Context, provider string, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
	if err := ctx.Err(); err != nil {
		return commerce.DiscountQuote{}, err
	}
	if provider != "example.plan-base-initial" || in.Operation != "new" || in.BasePriceMinor != 12000 || in.Currency != "CNY" {
		return commerce.DiscountQuote{}, commerce.ErrDiscountQuoteInvalid
	}
	terms := `{"discount_minor":2000,"scope":"initial_only","fixture":"plan-base-sync"}`
	hash, err := commerce.DiscountTermsHash(terms)
	if err != nil {
		return commerce.DiscountQuote{}, err
	}
	return commerce.DiscountQuote{
		Provider: commerce.DiscountProviderIdentity{PluginID: provider, SigningKeyFingerprint: strings.Repeat("a", 64), PackageDigest: strings.Repeat("b", 64), Generation: 1, ConfigRevision: 1},
		Offer:    commerce.DiscountOffer{Status: "available", InputFingerprint: in.InputFingerprint, Currency: in.Currency, DiscountMinor: 2000, ExpiresAt: time.Now().UTC().Add(time.Hour), Agreement: &commerce.DiscountAgreement{ID: "plan-base-initial", Version: "1", Scope: "initial_only", TermsJSON: terms, TermsHash: hash}},
	}, nil
}

func planBaseQuotaInitialDiscountFixture(t *testing.T, f orderFixture, used int64) (orderFixture, model.Order) {
	t.Helper()
	attachOrderPublishEndpoint(t, f)
	if err := f.h.db.Model(&f.planRecord).Updates(map[string]any{"traffic_bytes": 100, "reset_policy": 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&f.skuRecord).Updates(map[string]any{"price_cents": 12000, "billing_unit": "once", "billing_value": 1, "billing_mode": "one_time", "renewal_effect": "add_quota_only"}).Error; err != nil {
		t.Fatal(err)
	}
	fundTestWallet(t, f, 30000)
	orders := commerce.OrderCreation{Repository: commercestore.OrderCreation{DB: f.h.db, Discounts: planBaseQuotaInitialQuoter{}}}
	request := commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, IdempotencyKey: "plan-base-initial-purchase", Discount: &commerce.OrderDiscountSelection{ProviderID: "example.plan-base-initial", Selection: "initial-only"}}
	preview, err := orders.Preview(t.Context(), 1, request)
	if err != nil || preview.BasePriceMinor != 12000 || preview.DiscountAmount != 2000 || preview.PayableAmount != 10000 {
		t.Fatal("preview actual initial-only purchase", preview, err)
	}
	request.QuoteFingerprint = preview.QuoteFingerprint
	created, err := orders.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal("accept actual initial-only purchase", err)
	}
	payments := fundingService(f)
	if _, err := payments.Configure(t.Context(), 1, created.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal("allocate initial-only purchase wallet funding", err)
	}
	if _, err := payments.Pay(t.Context(), 1, created.ID); err != nil {
		t.Fatal("fulfill initial-only purchase from wallet", err)
	}
	var root model.Order
	if err := f.h.db.First(&root, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	policy, err := commerce.DecodePurchaseSnapshot(root.PurchaseSnapshot)
	if err != nil || policy.Discount == nil || policy.Discount.Version != 1 || policy.Discount.Quote.Offer.Agreement == nil || policy.Discount.Quote.Offer.Agreement.Scope != "initial_only" || commerce.ValidatePurchaseDiscount(commerce.Order(root), policy) != nil {
		t.Fatal("purchase did not persist a verifiable accepted initial-only agreement", err)
	}
	var fact commerce.EntitlementValueSnapshot
	if err := json.Unmarshal([]byte(root.EntitlementValueSnapshot), &fact); err != nil || fact.AllocationModel != commerce.PerpetualQuotaAllocation || !fact.PaymentVerified || fact.ActualPaidMinor != 10000 || root.PaidAmount != 10000 || root.AmountCents != 12000 || root.DiscountAmount != 2000 || root.PayableAmount != 10000 || len(fact.PaidPeriods) != 0 {
		t.Fatalf("initial-only purchase lost its actual cash/base-price distinction: %+v %v", fact, err)
	}
	if used > 0 {
		var credential model.ProtocolCredential
		if err := f.h.db.Where("subscription_id = ? AND current_scope = ?", root.SubscriptionID, 1).First(&credential).Error; err != nil {
			t.Fatal(err)
		}
		accounting := metering.CompletionAccounting{Repository: meteringstore.CompletionAccounting{DB: f.h.db, Cipher: f.h.credentialCipher, EnqueuePublications: networkstore.EnqueueSubscriptionPublications}}
		if _, _, err := accounting.Complete(t.Context(), metering.CompletedFlow{NodeID: credential.NodeID, CoreInstanceID: "initial-only-plan-base-core", EventID: "initial-only-plan-base-use", EventType: "flow.completed", Sequence: 1, FlowID: "initial-only-plan-base-flow", PrincipalKey: credential.PrincipalKey, BytesDown: used, OccurredAt: time.Now().UTC()}); err != nil {
			t.Fatal("meter initial-only purchase", err)
		}
	}
	return f, root
}
