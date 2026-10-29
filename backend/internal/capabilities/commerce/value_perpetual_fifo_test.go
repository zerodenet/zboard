package commerce

import (
	"testing"
	"time"
)

func perpetualAddition(previous EntitlementValueSnapshot, id uint, used, quota, paid int64) EntitlementValueSnapshot {
	before := previous.After
	before.FlowUsed = used
	after := before
	after.FlowTotal += quota
	after.ResetQuotaBytes = quota
	at := previous.CapturedAt.Add(time.Hour)
	return EntitlementValueSnapshot{Version: 1, OrderID: id, SubscriptionID: previous.SubscriptionID, QuotaEventID: id, QuotaEventAt: at, CapturedAt: at, Currency: "CNY", ActualPaidMinor: paid, PaymentVerified: true, PaymentBasis: "verified_cash_or_wallet", Kind: "renewal", Mode: "perpetual_quota_addition", AllocationModel: PerpetualQuotaFIFOAllocation, BillingUnit: "once", BillingValue: 1, TermStartAt: at, TermEndAt: after.EndAt, Before: &before, After: after}
}

func TestPerpetualFIFOUsesOriginalLotPrices(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		used, paid, want, first, second int64
	}{
		{"old remainder untouched by new price", 20, 10000, 28000, 8000, 20000},
		{"old lot consumed first", 110, 10000, 16000, 0, 16000},
		{"all consumed", 150, 10000, 0, 0, 0},
		{"overuse not debt", 170, 10000, 0, 0, 0},
		{"free lot consumed first", 110, 0, 16000, 0, 16000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := perpetualValuePurchase()
			root.ActualPaidMinor = tc.paid
			add := perpetualAddition(root, 2, 20, 50, 20000)
			current := add.After
			current.FlowUsed = tc.used
			out, err := ProjectEntitlementValue([]EntitlementValueSnapshot{add, root}, current, "CNY", add.CapturedAt.Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			assertProjectionConserves(t, out, tc.want)
			if out.Sources[root.OrderID] != tc.first || out.Sources[add.OrderID] != tc.second || out.FutureMinor != 0 || out.ValuationModel != PerpetualQuotaFIFOAllocation {
				t.Fatal(out)
			}
		})
	}
}

func TestPerpetualFIFOMultiplePurchasesKeepBatchRoundingAndNoRetroactiveUse(t *testing.T) {
	root := perpetualValuePurchase()
	add := perpetualAddition(root, 2, 20, 50, 20000)
	third := perpetualAddition(add, 3, 120, 200, 30000)
	current := third.After
	current.FlowUsed = 180
	out, err := ProjectEntitlementValue([]EntitlementValueSnapshot{root, add, third}, current, "CNY", third.CapturedAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	assertProjectionConserves(t, out, 25500)
	if len(out.Sources) != 1 || out.Sources[3] != 25500 {
		t.Fatal(out)
	}
	// Do not rebase a rounded remainder when another purchase arrives.
	root.After.FlowTotal, root.After.ResetQuotaBytes, root.ActualPaidMinor = 3, 3, 1
	add = perpetualAddition(root, 2, 1, 1, 0)
	current = add.After
	current.FlowUsed = 2
	out, err = ProjectEntitlementValue([]EntitlementValueSnapshot{root, add}, current, "CNY", add.CapturedAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	assertProjectionConserves(t, out, 0)
	root = perpetualValuePurchase()
	add = perpetualAddition(root, 2, 120, 50, 20000)
	out, err = ProjectEntitlementValue([]EntitlementValueSnapshot{root, add}, add.After, "CNY", add.CapturedAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	assertProjectionConserves(t, out, 20000)
}

func TestPerpetualFIFOAcceptsCapturedLegacyAdditionsAndRejectsUnprovenHistory(t *testing.T) {
	for _, kind := range []string{"renewal", "traffic_pack"} {
		t.Run(kind, func(t *testing.T) {
			root := perpetualValuePurchase()
			add := perpetualAddition(root, 2, 20, 50, 20000)
			add.Kind, add.Mode, add.AllocationModel = kind, "unsupported_legacy_effect", "uniform_paid_units_v1"
			if kind == "traffic_pack" {
				add.AllocationModel = "legacy_value_requires_review"
			}
			out, err := ProjectEntitlementValue([]EntitlementValueSnapshot{root, add}, add.After, "CNY", add.CapturedAt.Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			assertProjectionConserves(t, out, 28000)
		})
	}
	for _, name := range []string{"unverified", "missing policy", "duplicate order", "duplicate event", "currency", "missing before", "reset", "unrecorded delta", "usage rollback", "kind", "paid periods"} {
		t.Run(name, func(t *testing.T) {
			root := perpetualValuePurchase()
			add := perpetualAddition(root, 2, 20, 50, 20000)
			current := add.After
			switch name {
			case "unverified":
				add.PaymentVerified = false
			case "missing policy":
				add.AllocationModel = "purchase_value_policy_unavailable"
			case "duplicate order":
				add.OrderID = root.OrderID
			case "duplicate event":
				add.QuotaEventID = root.QuotaEventID
			case "currency":
				add.Currency = "USD"
			case "missing before":
				add.Before = nil
			case "reset":
				add.After.CycleStartUsed = 20
			case "unrecorded delta":
				current.FlowTotal++
			case "usage rollback":
				current.FlowUsed = 19
			case "kind":
				add.Kind = "traffic_reset"
			case "paid periods":
				add.PaidPeriods = []PaidValuePeriod{{AmountMinor: 20000}}
			}
			if _, err := ProjectEntitlementValue([]EntitlementValueSnapshot{root, add}, current, "CNY", add.CapturedAt.Add(time.Hour)); err == nil {
				t.Fatal("unproven batch became refundable")
			}
		})
	}
}

func TestPerpetualFIFOPreservesEstablishedSingleLotQuotaSyncAndRejectsUnknownMixedAdjustment(t *testing.T) {
	root := perpetualValuePurchase()
	add := perpetualAddition(root, 2, 60, 50, 20000)
	// The plan quota was synchronized from 100 to 200 before any second lot.
	// Existing single-lot semantics: 140/200 of its original 10000 remains.
	add.Before.FlowTotal = 200
	add.After.FlowTotal = 250
	out, err := ProjectEntitlementValue([]EntitlementValueSnapshot{root, add}, add.After, "CNY", add.CapturedAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	assertProjectionConserves(t, out, 27000)
	third := perpetualAddition(add, 3, 60, 50, 1000)
	third.Before.FlowTotal++
	third.After.FlowTotal++
	if _, err := ProjectEntitlementValue([]EntitlementValueSnapshot{root, add, third}, third.After, "CNY", third.CapturedAt.Add(time.Hour)); err == nil {
		t.Fatal("unattributed mixed-lot adjustment became paid value")
	}
}
