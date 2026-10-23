package commerce

import (
	"encoding/json"
	"testing"
	"time"
)

func operationDiscountFixture(t *testing.T) (Order, PurchaseSnapshot) {
	t.Helper()
	order, policy := purchasedDiscountFixture(t)
	target := uint(9)
	order.OrderType = "upgrade"
	order.TargetSubscriptionID = &target
	fact := SubscriptionChangeSnapshot{Version: 2, FullTerm: true, EntitlementEpoch: 4, Residual: 15000, Applied: 10000, Surplus: 5000, Credit: 10000, Sources: map[uint]int64{99: 15000}, PlanID: 3, PlanSKUID: 4, EndAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), SourceOrderID: 99, QuotaEventID: 5, QuotedAt: policy.Discount.AcceptedAt}
	raw, _ := json.Marshal(fact)
	order.ChangeSnapshot = string(raw)
	in, err := NewOperationDiscountContext(order, policy, "selected-single-order")
	if err != nil {
		t.Fatal(err)
	}
	policy.Discount.Version = 3
	policy.Discount.Context = in
	policy.Discount.Quote.Offer.InputFingerprint = in.InputFingerprint
	policy.Discount.Quote.Offer.Agreement.Scope = OperationDiscountScope
	return order, policy
}
func TestOperationDiscountReaderBindsTargetMoneyAndLockedBase(t *testing.T) {
	order, policy := operationDiscountFixture(t)
	if err := ValidatePurchaseDiscount(order, policy); err != nil {
		t.Fatal(err)
	}
	before := order
	before.PayableAmount = before.AmountCents
	before.DiscountAmount = 0
	var fact SubscriptionChangeSnapshot
	if err := json.Unmarshal([]byte(before.ChangeSnapshot), &fact); err != nil {
		t.Fatal(err)
	}
	fact.Applied = 12000
	fact.Credit = 12000
	fact.Surplus = 3000
	fact.QuotedAt = fact.QuotedAt.Add(time.Second)
	fact.ValueFingerprint = "later-identical-cent-evaluation"
	raw, _ := json.Marshal(fact)
	before.ChangeSnapshot = string(raw)
	in, err := NewOperationDiscountContext(before, policy, policy.Discount.Context.Selection)
	if err != nil || in.InputFingerprint != policy.Discount.Context.InputFingerprint {
		t.Fatalf("discount allocation changed target identity %+v %v", in, err)
	}
	for _, kind := range []string{"buyer", "target", "base", "discount", "payable", "traffic", "source", "residual", "epoch", "scope", "operation", "unknown-field"} {
		t.Run(kind, func(t *testing.T) {
			order, policy := operationDiscountFixture(t)
			switch kind {
			case "buyer":
				order.UserID++
			case "target":
				other := uint(8)
				order.TargetSubscriptionID = &other
			case "base":
				order.AmountCents++
			case "discount":
				order.DiscountAmount++
			case "payable":
				order.PayableAmount++
			case "traffic":
				order.TrafficBytes++
			case "scope":
				policy.Discount.Quote.Offer.Agreement.Scope = "recurring"
			case "operation":
				order.OrderType = "renewal"
			case "unknown-field":
				order.ChangeSnapshot = order.ChangeSnapshot[:len(order.ChangeSnapshot)-1] + `,"IgnoredAuthority":1}`
			default:
				var fact SubscriptionChangeSnapshot
				_ = json.Unmarshal([]byte(order.ChangeSnapshot), &fact)
				if kind == "source" {
					fact.Sources = map[uint]int64{100: 15000}
				} else if kind == "residual" {
					fact.Residual++
					fact.Surplus++
				} else {
					fact.EntitlementEpoch++
				}
				raw, _ := json.Marshal(fact)
				order.ChangeSnapshot = string(raw)
			}
			if ValidatePurchaseDiscount(order, policy) == nil {
				t.Fatal("changed commercial or target evidence accepted")
			}
		})
	}
	for _, op := range []string{"new", "renewal"} {
		_, policy := operationDiscountFixture(t)
		in := policy.Discount.Context
		in.Operation = op
		if op == "new" {
			in.TargetSubscriptionID = 0
		}
		quote := policy.Discount.Quote
		quote.Offer.InputFingerprint = in.InputFingerprint
		if ValidateDiscountQuote(in, quote, policy.Discount.AcceptedAt) == nil {
			t.Fatal("one-order operation scope used for", op)
		}
	}
}

func TestOperationDiscountReaderSupportsResetAndPackWithoutRecurringConsent(t *testing.T) {
	for _, operation := range []string{"traffic_reset", "traffic_pack"} {
		t.Run(operation, func(t *testing.T) {
			order, policy := purchasedDiscountFixture(t)
			target := uint(42)
			order.OrderType = operation
			order.TargetSubscriptionID = &target
			order.BillingUnit = "once"
			order.BillingValue = 1
			policy.BillingUnit = order.BillingUnit
			policy.BillingValue = 1
			if operation == "traffic_reset" {
				order.RenewalEffect = "reset_current_quota"
				policy.RenewalEffect = order.RenewalEffect
				policy.ValueAllocationModel = "paid_reset_continuity_v1"
				// A legacy unknown old SKU is preserved as unknown, never inferred from
				// the new reset SKU. Price is the explicitly accepted reset operation.
				facts := SubscriptionChangeSnapshot{PlanID: order.PlanID, PlanSKUID: 0, EndAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), CycleStartUsed: 5, ResetQuota: 100, QuotaEventID: 8, QuotedAt: policy.Discount.AcceptedAt}
				raw, _ := json.Marshal(facts)
				order.ChangeSnapshot = string(raw)
			}
			in, err := NewOperationDiscountContext(order, policy, "single-operation")
			if err != nil {
				t.Fatal(err)
			}
			policy.Discount.Version = 3
			policy.Discount.Context = in
			policy.Discount.Quote.Offer.InputFingerprint = in.InputFingerprint
			policy.Discount.Quote.Offer.Agreement.Scope = OperationDiscountScope
			if in.Agreement != nil {
				t.Fatal("one-order offer inherited recurring consent")
			}
			if err := ValidatePurchaseDiscount(order, policy); err != nil {
				t.Fatal(err)
			}
			order.Channel = "wallet"
			order.ID = 123
			order.TradeNo = "generated-after-acceptance"
			order.SubscriptionID = target
			if err := ValidatePurchaseDiscount(order, policy); err != nil {
				t.Fatal("funding/generated identities changed commercial authority", err)
			}
			policy.Discount.Quote.Offer.Agreement.Scope = "initial_only"
			if ValidatePurchaseDiscount(order, policy) == nil {
				t.Fatal("first-purchase scope was reused on target operation")
			}
		})
	}
}
