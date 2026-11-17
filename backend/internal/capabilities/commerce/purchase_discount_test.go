package commerce

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func purchasedDiscountFixture(t *testing.T) (Order, PurchaseSnapshot) {
	t.Helper()
	order := Order{UserID: 7, PlanID: 1, PlanSKUID: 2, OrderType: "new", AmountCents: 12000, DiscountAmount: 2000, PayableAmount: 10000, Currency: "CNY", BillingUnit: "month", BillingValue: 1, RenewalEffect: "extend_only", Channel: "manual", TrafficBytes: 1024, DeviceLimit: 1}
	policy := PurchaseSnapshot{Version: 1, ValueAllocationModel: "uniform_paid_units_v1", BasePriceMinor: 12000, Currency: "CNY", BillingUnit: "month", BillingValue: 1, RenewalEffect: "extend_only", Renewable: true}
	in, err := NewPurchaseDiscountContext(order, policy, "first")
	if err != nil {
		t.Fatal(err)
	}
	acceptedAt := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	terms := `{"discount_minor":2000}`
	hash, _ := DiscountTermsHash(terms)
	quote := DiscountQuote{Provider: DiscountProviderIdentity{PluginID: "example.quote", SigningKeyFingerprint: strings.Repeat("a", 64), PackageDigest: strings.Repeat("b", 64), Generation: 1, ConfigRevision: 1}, Offer: DiscountOffer{Status: "available", InputFingerprint: in.InputFingerprint, Currency: in.Currency, DiscountMinor: 2000, ExpiresAt: acceptedAt.Add(time.Minute), Agreement: &DiscountAgreement{ID: "first", Version: "1", Scope: "initial_only", TermsJSON: terms, TermsHash: hash}}}
	policy.Discount = &PurchaseDiscountSnapshot{Version: 1, Context: in, Quote: quote, FinalPriceMinor: 10000, AcceptedAt: acceptedAt}
	return order, policy
}

func TestPurchaseDiscountSnapshotConservesBaseReductionAndFinal(t *testing.T) {
	order, policy := purchasedDiscountFixture(t)
	raw, _ := json.Marshal(policy)
	t.Logf("complete minimal discount purchase_snapshot JSON: %d bytes", len(raw))
	if err := ValidatePurchaseDiscount(order, policy); err != nil {
		t.Fatal(err)
	}
	// Locked acceptance remains valid after its original offer has expired.
	if err := ValidateDiscountQuote(policy.Discount.Context, policy.Discount.Quote, policy.Discount.Quote.Offer.ExpiresAt.Add(time.Hour)); err == nil {
		t.Fatal("old quote accepted as new")
	}
	for _, variant := range []string{"base", "discount", "payable", "final", "buyer", "sku", "currency", "period", "traffic", "policy", "terms", "scope", "status", "identity", "accepted-at", "context", "operation", "missing"} {
		t.Run(variant, func(t *testing.T) {
			order, policy := purchasedDiscountFixture(t)
			switch variant {
			case "base":
				order.AmountCents = 10000
			case "discount":
				order.DiscountAmount = 1
			case "payable":
				order.PayableAmount = 12000
			case "final":
				policy.Discount.FinalPriceMinor = 12000
			case "buyer":
				order.UserID++
			case "sku":
				order.PlanSKUID++
			case "currency":
				order.Currency = "USD"
			case "period":
				order.BillingValue++
			case "traffic":
				order.TrafficBytes++
			case "policy":
				policy.Renewable = false
			case "terms":
				policy.Discount.Quote.Offer.Agreement.TermsJSON = `{"discount_minor":0}`
			case "scope":
				policy.Discount.Quote.Offer.Agreement.Scope = "recurring"
			case "status":
				policy.Discount.Quote.Offer.Status = "requires_confirmation"
			case "identity":
				policy.Discount.Quote.Provider.SigningKeyFingerprint = "bad"
			case "accepted-at":
				policy.Discount.AcceptedAt = policy.Discount.Quote.Offer.ExpiresAt
			case "context":
				policy.Discount.Context.Selection = "other"
			case "operation":
				order.OrderType = "renewal"
			case "missing":
				policy.Discount = nil
			}
			if err := ValidatePurchaseDiscount(order, policy); err == nil {
				t.Fatal("inconsistent monetary/authority evidence accepted")
			}
		})
	}
}

func TestPurchaseDiscountSurvivesPaymentChannelSelection(t *testing.T) {
	order, policy := purchasedDiscountFixture(t)
	for _, channel := range []string{"manual", "wallet", "verified-provider"} {
		order.Channel = channel
		if err := ValidatePurchaseDiscount(order, policy); err != nil {
			t.Fatalf("payment allocation changed the locked commercial agreement: %s %v", channel, err)
		}
	}
	order.PayableAmount++
	if err := ValidatePurchaseDiscount(order, policy); err == nil {
		t.Fatal("payment channel independence allowed a price change")
	}
}

func TestPurchaseDiscountQuoteBindsMaterialEvidenceAndAllowsFreshExpiry(t *testing.T) {
	_, original := purchasedDiscountFixture(t)
	token := func(policy PurchaseSnapshot) string {
		raw, _ := json.Marshal(PurchaseQuotePolicy(policy))
		return string(raw)
	}
	for _, variant := range []string{"expiry", "message", "accepted-at", "provider", "signer", "package", "generation", "config", "amount", "final", "context", "terms"} {
		t.Run(variant, func(t *testing.T) {
			_, changed := purchasedDiscountFixture(t)
			switch variant {
			case "expiry":
				changed.Discount.Quote.Offer.ExpiresAt = changed.Discount.Quote.Offer.ExpiresAt.Add(time.Minute)
			case "message":
				changed.Discount.Quote.Offer.Message = "fresh explanation"
			case "accepted-at":
				changed.Discount.AcceptedAt = changed.Discount.AcceptedAt.Add(time.Second)
			case "provider":
				changed.Discount.Quote.Provider.PluginID = "other.quote"
			case "signer":
				changed.Discount.Quote.Provider.SigningKeyFingerprint = strings.Repeat("c", 64)
			case "package":
				changed.Discount.Quote.Provider.PackageDigest = strings.Repeat("c", 64)
			case "generation":
				changed.Discount.Quote.Provider.Generation++
			case "config":
				changed.Discount.Quote.Provider.ConfigRevision++
			case "amount":
				changed.Discount.Quote.Offer.DiscountMinor++
			case "final":
				changed.Discount.FinalPriceMinor++
			case "context":
				changed.Discount.Context.InputFingerprint = strings.Repeat("c", 64)
			case "terms":
				changed.Discount.Quote.Offer.Agreement.Version = "2"
			}
			same := token(original) == token(changed)
			transient := variant == "expiry" || variant == "message" || variant == "accepted-at"
			if same != transient {
				t.Fatalf("material/transient binding: %s same=%t", variant, same)
			}
		})
	}
}

func TestPurchaseDiscountSnapshotRejectsUnknownMalformedAndUnboundedJSON(t *testing.T) {
	_, policy := purchasedDiscountFixture(t)
	raw, _ := json.Marshal(policy)
	for _, bad := range []string{
		string(raw[:len(raw)-1]) + `,"unknown_policy":true}`,
		strings.Replace(string(raw), `"final_price_minor":10000`, `"final_price_minor":10000,"payment_authority":true`, 1),
		string(raw) + ` {}`,
		string(raw[:len(raw)-1]),
		"null",
		strings.Repeat(" ", MaxPurchaseSnapshotBytes+1),
	} {
		if _, err := DecodePurchaseSnapshot(bad); err == nil {
			t.Fatal("unknown/malformed/unbounded discount snapshot accepted")
		}
	}
	if _, err := DecodePurchaseSnapshot(string(raw)); err != nil {
		t.Fatal(err)
	}
}
