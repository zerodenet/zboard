package commercestore

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func automaticApprovalPureFixture(t *testing.T) (model.Order, checkoutDiscount) {
	t.Helper()
	target := uint(2)
	policy := commerce.PurchaseSnapshot{Version: 1, BasePriceMinor: 12000, Currency: "CNY", BillingUnit: "month", BillingValue: 1, RenewalEffect: "extend_only", Renewable: true, ValueAllocationModel: "uniform_paid_units_v1", RenewalTarget: &commerce.RenewalTargetSnapshot{EndAt: time.Now().UTC()}}
	raw, _ := json.Marshal(policy)
	order := model.Order{UserID: 1, PlanID: 1, PlanSKUID: 1, OrderType: "renewal", TargetSubscriptionID: &target, AmountCents: 12000, PayableAmount: 10000, DiscountAmount: 2000, Currency: "CNY", BillingUnit: "month", BillingValue: 1, RenewalEffect: "extend_only", TrafficBytes: 1024, DeviceLimit: 1, PurchaseSnapshot: string(raw)}
	quote := checkoutTestQuote(commerce.DiscountQuoteContext{}, time.Now().UTC())
	quote.Offer.Agreement.Scope = "recurring"
	source := &commerce.DiscountSourceSnapshot{Version: 1, SubscriptionID: target, RootOrderID: 1, BaseOrderID: 1, AgreementOrderID: 1, Fingerprint: strings.Repeat("c", 64), ProviderID: quote.Provider.PluginID, SigningKeyFingerprint: quote.Provider.SigningKeyFingerprint, Agreement: *quote.Offer.Agreement}
	in, err := commerce.NewRecurringDiscountContext(commerce.Order(order), policy, "", source)
	if err != nil {
		t.Fatal(err)
	}
	quote.Offer.InputFingerprint = in.InputFingerprint
	quote.Offer.Currency = "CNY"
	return order, checkoutDiscount{Version: 2, Source: source, Context: in, Quote: quote}
}

func TestAutomaticRecurringApprovalPureStableMaterialAndStrictEvidence(t *testing.T) {
	order, accepted := automaticApprovalPureFixture(t)
	raw, digest, err := automaticRecurringApproval(order, accepted)
	if err != nil {
		t.Fatal(err)
	}
	accepted.Source.AgreementOrderID = 99
	accepted.Source.EntitlementEpoch++
	accepted.Source.Fingerprint = strings.Repeat("d", 64)
	accepted.Context.InputFingerprint = strings.Repeat("e", 64)
	accepted.AcceptedAt = time.Now().Add(time.Hour)
	accepted.Quote.Offer.ExpiresAt = time.Now().Add(2 * time.Hour)
	accepted.Quote.Offer.Message = "new observation"
	var policy commerce.PurchaseSnapshot
	_ = json.Unmarshal([]byte(order.PurchaseSnapshot), &policy)
	policy.RenewalTarget.EndAt = policy.RenewalTarget.EndAt.AddDate(1, 0, 0)
	encoded, _ := json.Marshal(policy)
	order.PurchaseSnapshot = string(encoded)
	other, otherDigest, err := automaticRecurringApproval(order, accepted)
	if err != nil || raw != other || digest != otherDigest {
		t.Fatal("normal source/date/clock advancement changed continuing consent", err)
	}
	for _, kind := range []string{"price", "discount", "config", "package", "generation", "traffic", "selection", "terms"} {
		t.Run(kind, func(t *testing.T) {
			o, a := automaticApprovalPureFixture(t)
			switch kind {
			case "price":
				o.AmountCents++
			case "discount":
				a.Quote.Offer.DiscountMinor++
			case "config":
				a.Quote.Provider.ConfigRevision++
			case "package":
				a.Quote.Provider.PackageDigest = strings.Repeat("f", 64)
			case "generation":
				a.Quote.Provider.Generation++
			case "traffic":
				o.TrafficBytes++
			case "selection":
				a.Context.Selection = "other"
			case "terms":
				agreement := *a.Quote.Offer.Agreement
				agreement.TermsJSON = `{"discount_minor":2000,"revision":2}`
				agreement.TermsHash, _ = commerce.DiscountTermsHash(agreement.TermsJSON)
				a.Quote.Offer.Agreement = &agreement
			}
			_, changed, err := automaticRecurringApproval(o, a)
			if err == nil && changed == digest {
				t.Fatal("material change preserved old debit consent")
			}
		})
	}
	for _, kind := range []string{"unknown", "missing", "money", "version", "trailing", "duplicate", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			var fields map[string]any
			_ = json.Unmarshal([]byte(raw), &fields)
			switch kind {
			case "unknown":
				fields["future_authority"] = true
			case "missing":
				delete(fields, "discount_minor")
			case "money":
				fields["payable_minor"] = 12000
			case "version":
				fields["version"] = 0
			}
			b, _ := json.Marshal(fields)
			bad := string(b)
			switch kind {
			case "trailing":
				bad = raw + ` {}`
			case "duplicate":
				bad = strings.Replace(raw, `"version":2`, `"version":2,"version":2`, 1)
			case "oversize":
				bad = strings.Repeat(" ", commerce.MaxPurchaseSnapshotBytes+1)
			}
			if _, err := validateAutomaticRecurringApproval(bad, 1, 2, 1, 1); err == nil {
				t.Fatal("malformed approval accepted")
			}
		})
	}
}
