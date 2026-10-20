package commerce

import (
	"encoding/json"
	"strings"
	"testing"
)

func recurringPurchaseFixture(t *testing.T) (Order, PurchaseSnapshot) {
	order, policy := purchasedDiscountFixture(t)
	policy.Discount.Version = 2
	policy.Discount.Quote.Offer.Agreement.Scope = "recurring"
	return order, policy
}

func TestRecurringV2NewAndV1InitialRemainSeparate(t *testing.T) {
	oldOrder, oldPolicy := purchasedDiscountFixture(t)
	before, _ := json.Marshal(oldPolicy)
	if oldPolicy.Discount.Context.InputFingerprint != "7ed51036ce8a76af0fbc7ebc1cff5af87c9692b5ae7182f2f9505e3f33b57079" {
		t.Fatal("V1 frozen input fingerprint changed")
	}
	if ValidatePurchaseDiscount(oldOrder, oldPolicy) != nil {
		t.Fatal("V1 invalid")
	}
	order, policy := recurringPurchaseFixture(t)
	in, err := NewRecurringDiscountContext(order, policy, policy.Discount.Context.Selection, nil)
	if err != nil || in.InputFingerprint != oldPolicy.Discount.Context.InputFingerprint || ValidatePurchaseDiscount(order, policy) != nil {
		t.Fatal("V2 recurring new facts changed the V1 input representation", err)
	}
	after, _ := json.Marshal(oldPolicy)
	if string(before) != string(after) || strings.Contains(string(after), `"source"`) {
		t.Fatal("V1 evidence representation changed")
	}
	policy.Discount.Version = 1
	if ValidatePurchaseDiscount(order, policy) == nil {
		t.Fatal("recurring evidence accepted by V1")
	}
}

func TestRecurringV2RenewalBindsSourceTargetAndPriorAgreement(t *testing.T) {
	order, policy := recurringPurchaseFixture(t)
	target := uint(9)
	order.OrderType = "renewal"
	order.TargetSubscriptionID = &target
	policy.RenewalTarget = &RenewalTargetSnapshot{EndAt: policy.Discount.AcceptedAt.AddDate(0, 1, 0), PlanSKUID: order.PlanSKUID}
	source := &DiscountSourceSnapshot{Version: 1, SubscriptionID: target, RootOrderID: 10, BaseOrderID: 10, AgreementOrderID: 11, Fingerprint: strings.Repeat("c", 64), EntitlementEpoch: 2, ProviderID: policy.Discount.Quote.Provider.PluginID, SigningKeyFingerprint: policy.Discount.Quote.Provider.SigningKeyFingerprint, Agreement: *policy.Discount.Quote.Offer.Agreement}
	in, err := NewRecurringDiscountContext(order, policy, "", source)
	if err != nil || in.PriceSourceOrderID != 10 || in.TargetSubscriptionID != target || in.Agreement == nil || in.Agreement.TermsHash != source.Agreement.TermsHash {
		t.Fatal(in, err)
	}
	policy.Discount.Context = in
	policy.Discount.Source = source
	policy.Discount.Quote.Offer.InputFingerprint = in.InputFingerprint
	if err := ValidatePurchaseDiscount(order, policy); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"source", "epoch", "signer", "provider", "base", "money", "agreement", "target", "scope", "confirmation", "missing-source"} {
		t.Run(variant, func(t *testing.T) {
			raw, _ := json.Marshal(policy)
			var changed PurchaseSnapshot
			if json.Unmarshal(raw, &changed) != nil {
				t.Fatal("fixture")
			}
			o := order
			switch variant {
			case "source":
				changed.Discount.Source.AgreementOrderID++
			case "epoch":
				changed.Discount.Source.EntitlementEpoch++
			case "signer":
				changed.Discount.Quote.Provider.SigningKeyFingerprint = strings.Repeat("d", 64)
			case "provider":
				changed.Discount.Quote.Provider.PluginID = "other.quote"
			case "base":
				changed.Discount.Source.BaseOrderID++
			case "money":
				o.PayableAmount = 12000
			case "agreement":
				changed.Discount.Quote.Offer.Agreement.Version = "2"
			case "target":
				changed.RenewalTarget.EndAt = changed.RenewalTarget.EndAt.AddDate(0, 0, 1)
			case "scope":
				changed.Discount.Quote.Offer.Agreement.Scope = "initial_only"
			case "confirmation":
				changed.Discount.Quote.Offer.Status = "requires_confirmation"
			case "missing-source":
				changed.Discount.Source = nil
			}
			if ValidatePurchaseDiscount(o, changed) == nil {
				t.Fatal("changed V2 authority accepted")
			}
		})
	}
}
