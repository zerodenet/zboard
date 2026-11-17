package commerce

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func operationDiscountLargeSourcesFixture(t *testing.T, count int) (Order, PurchaseSnapshot) {
	t.Helper()
	order, policy := operationDiscountFixture(t)
	var change SubscriptionChangeSnapshot
	if err := json.Unmarshal([]byte(order.ChangeSnapshot), &change); err != nil {
		t.Fatal(err)
	}
	change.Sources = make(map[uint]int64, count)
	for id := 1; id <= count; id++ {
		change.Sources[uint(id)] = 1
	}
	change.Residual = int64(count)
	change.Applied = min(change.Residual, order.PayableAmount)
	change.Credit = change.Applied
	change.Surplus = change.Residual - change.Applied
	encoded, err := json.Marshal(change)
	if err != nil {
		t.Fatal(err)
	}
	order.ChangeSnapshot = string(encoded)
	in, err := NewOperationDiscountContext(order, policy, policy.Discount.Context.Selection)
	if err != nil {
		t.Fatal("bounded captured sources could not obtain operation context", count, err)
	}
	policy.Discount.Context = in
	policy.Discount.Quote.Offer.InputFingerprint = in.InputFingerprint
	return order, policy
}

func TestOperationDiscountLargeSourcesPreserveLegacyFingerprints(t *testing.T) {
	// Canonical serialized input remains exactly the pre-change v3 shape; only
	// the obsolete >1000 rejection changes. These are compatibility goldens.
	order, policy := operationDiscountFixture(t)
	if got := policy.Discount.Context.InputFingerprint; got != "e912c882b271bf3fb078fb65966aa20ebba2d21121e39b9041d39d6e526ae6ae" {
		t.Fatal("legacy one-source fingerprint changed", got)
	}
	if err := ValidatePurchaseDiscount(order, policy); err != nil {
		t.Fatal("legacy accepted purchase changed", err)
	}
	_, policy = operationDiscountLargeSourcesFixture(t, 1000)
	if got := policy.Discount.Context.InputFingerprint; got != "80ad65f7c32b4f6d6248a502b1efea804997ffcf3dcd4904c3b22de6ef2445c3" {
		t.Fatal("legacy 1000-source fingerprint changed", got)
	}
}

func TestOperationDiscountLargeSourcesBindEverySourceWithinByteBudget(t *testing.T) {
	order, policy := operationDiscountLargeSourcesFixture(t, 1200)
	if err := ValidatePurchaseDiscount(order, policy); err != nil {
		t.Fatal("1200 bounded source purchase was rejected", err)
	}
	contextBody, err := json.Marshal(policy.Discount.Context)
	if err != nil || len(contextBody) > 2048 || strings.Contains(string(contextBody), "Sources") || strings.Contains(string(contextBody), "sources") {
		t.Fatal("provider context materialized full internal source history", len(contextBody), err)
	}
	var original SubscriptionChangeSnapshot
	if err := json.Unmarshal([]byte(order.ChangeSnapshot), &original); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"late-id", "late-amount", "negative", "missing"} {
		t.Run(mutation, func(t *testing.T) {
			var changed SubscriptionChangeSnapshot
			if err := json.Unmarshal([]byte(order.ChangeSnapshot), &changed); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "late-id":
				delete(changed.Sources, 1200)
				changed.Sources[1201] = 1
			case "late-amount":
				changed.Sources[1200]++
			case "negative":
				changed.Sources[1200] = -1
			case "missing":
				delete(changed.Sources, 1200)
			}
			raw, _ := json.Marshal(changed)
			tampered := order
			tampered.ChangeSnapshot = string(raw)
			if ValidatePurchaseDiscount(tampered, policy) == nil {
				t.Fatal("changed source retained the accepted authority")
			}
		})
	}
	if len(original.Sources) != 1200 || original.Sources[1200] != 1 {
		t.Fatal("caller source history was changed")
	}
	// The parser's original serialized byte boundary remains inclusive at
	// 256KiB and rejects even whitespace beyond it before JSON decoding.
	boundary := order
	boundary.ChangeSnapshot += strings.Repeat(" ", (256<<10)-len(boundary.ChangeSnapshot))
	if len(boundary.ChangeSnapshot) != 256<<10 || ValidatePurchaseDiscount(boundary, policy) != nil {
		t.Fatal("existing exact byte boundary was reduced")
	}
	boundary.ChangeSnapshot += " "
	if !errors.Is(ValidatePurchaseDiscount(boundary, policy), ErrDiscountQuoteInvalid) {
		t.Fatal("serialized source budget was bypassed")
	}
}
