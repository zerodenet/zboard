package commerce

import (
	"testing"
	"time"
)

func timedAdditionFact(root EntitlementValueSnapshot, before EntitlementValueState, id uint, at time.Time, paid, quota int64) EntitlementValueSnapshot {
	after := before
	after.FlowTotal += quota
	end := before.EndAt
	if before.NextResetAt != nil && before.NextResetAt.Before(end) {
		end = *before.NextResetAt
	}
	return EntitlementValueSnapshot{Version: 1, OrderID: id, SubscriptionID: root.SubscriptionID, QuotaEventID: id,
		QuotaEventAt: at, Currency: "CNY", ActualPaidMinor: paid, PaymentVerified: true, PaymentBasis: "verified_cash_or_wallet",
		Kind: "traffic_pack", Mode: "timed_quota_addition", CapturedAt: at, TermStartAt: at, TermEndAt: end,
		BillingUnit: "once", BillingValue: 1, AllocationModel: TimedQuotaAdditionAllocation, Before: &before, After: after}
}

func TestValueProjectionTimedAdditionPreservesConsumedAndFutureMoney(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	root := projectionPurchase(t, start, "year", 1, 12000, 2)
	before := root.After
	before.FlowUsed = 80
	at := start.AddDate(0, 0, 10)
	addition := timedAdditionFact(root, before, 2, at, 2000, 100)
	records := []EntitlementValueSnapshot{addition, root}
	out, err := ProjectEntitlementValue(records, addition.After, "CNY", at)
	if err != nil {
		t.Fatal(err)
	}
	assertProjectionConserves(t, out, 13200)
	if out.CurrentMinor != 2200 || out.FutureMinor != 11000 || out.Sources[1] != 11200 || out.Sources[2] != 2000 {
		t.Fatal("consumed old money revived or future allocations changed", out)
	}
	consumed := addition.After
	consumed.FlowUsed += 100 // Only 20 of the 120 post-purchase bytes remain.
	out, err = ProjectEntitlementValue(records, consumed, "CNY", at)
	if err != nil {
		t.Fatal(err)
	}
	assertProjectionConserves(t, out, 11367)
	if out.FutureMinor != 11000 || out.Sources[1] > root.ActualPaidMinor || out.Sources[2] > addition.ActualPaidMinor {
		t.Fatal("current consumption spent future funds", out)
	}
	// A second real-price batch retains the settled 367, rather than revaluing
	// the original consumed 1,000 or using a new catalog/unit price.
	two := timedAdditionFact(root, consumed, 3, at, 500, 100)
	records = append(records, two)
	out, err = ProjectEntitlementValue(records, two.After, "CNY", at)
	if err != nil {
		t.Fatal(err)
	}
	assertProjectionConserves(t, out, 11867)
	if out.CurrentMinor != 867 || out.FutureMinor != 11000 || out.Sources[3] != 500 {
		t.Fatal(out)
	}
	// The additional quota belongs to the existing cycle. Scheduled reset does
	// not create a second future-period refund for either addition.
	next := two.After
	next.CycleStartUsed = next.FlowUsed
	next.FlowTotal = next.FlowUsed + next.ResetQuotaBytes
	due := *next.NextResetAt
	following := due.AddDate(0, 1, 0)
	next.NextResetAt = &following
	out, err = ProjectEntitlementValue(records, next, "CNY", due)
	if err != nil {
		t.Fatal(err)
	}
	assertProjectionConserves(t, out, 11000)
	if out.Sources[2] != 0 || out.Sources[3] != 0 {
		t.Fatal("expired add-on gained future value", out)
	}
}

func TestValueProjectionTimedAdditionRejectsUnprovenOrChangedTerms(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	root := projectionPurchase(t, start, "month", 1, 1000, 2)
	for _, variant := range []string{"legacy", "unverified", "quota", "used", "cycle", "deadline", "plan", "reset", "negative", "duplicate"} {
		t.Run(variant, func(t *testing.T) {
			s := timedAdditionFact(root, root.After, 2, start, 200, 100)
			switch variant {
			case "legacy":
				s.Mode, s.AllocationModel = "unsupported_legacy_effect", "legacy_value_requires_review"
			case "unverified":
				s.PaymentVerified = false
			case "quota":
				s.After.FlowTotal = s.Before.FlowTotal
			case "used":
				s.After.FlowUsed++
			case "cycle":
				s.After.CycleStartUsed++
			case "deadline":
				s.TermEndAt = s.TermEndAt.Add(time.Hour)
			case "plan":
				s.After.PlanID++
			case "reset":
				s.After.ResetQuotaBytes++
			case "negative":
				s.ActualPaidMinor = -1
			}
			records := []EntitlementValueSnapshot{root, s}
			if variant == "duplicate" {
				records = append(records, s)
			}
			if _, err := ProjectEntitlementValue(records, s.After, "CNY", start); err == nil {
				t.Fatal("unproven or changed add-on was valued")
			}
		})
	}
}

func TestValueProjectionTimedAdditionSharedYearAndFreeQuota(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	root := projectionPurchase(t, start, "year", 1, 12000, 5)
	before := root.After
	before.FlowUsed = 80
	at := start.AddDate(0, 3, 0)
	for _, paid := range []int64{0, 2000} {
		addition := timedAdditionFact(root, before, 2, at, paid, 100)
		out, err := ProjectEntitlementValue([]EntitlementValueSnapshot{root, addition}, addition.After, "CNY", at)
		if err != nil {
			t.Fatal(err)
		}
		assertProjectionConserves(t, out, 2400+paid)
		if out.FutureMinor != 0 || out.Sources[1] != 2400 || out.Sources[2] != paid {
			t.Fatal("shared year or free quota revived consumed cash/future months", out)
		}
	}
}
