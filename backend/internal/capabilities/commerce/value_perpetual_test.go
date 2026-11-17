package commerce

import (
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

func perpetualValuePurchase() EntitlementValueSnapshot {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	return EntitlementValueSnapshot{Version: 1, OrderID: 1, SubscriptionID: 7, QuotaEventID: 1, QuotaEventAt: start, Currency: "CNY", ActualPaidMinor: 10000, PaymentVerified: true, PaymentBasis: "verified_cash_or_wallet", Kind: "new", Mode: "new_term", CapturedAt: start, TermStartAt: start, TermEndAt: entitlements.PerpetualEnd, BillingUnit: "once", BillingValue: 1, AllocationModel: PerpetualQuotaAllocation, After: EntitlementValueState{PlanID: 1, PlanSKUID: 1, StartAt: start, EndAt: entitlements.PerpetualEnd, Status: "active", FlowTotal: 100, ResetQuotaBytes: 100, ResetPolicy: 5}}
}

func TestPerpetualQuotaValueUsesOnlyActualPaidAndRemainingQuota(t *testing.T) {
	for _, test := range []struct {
		name             string
		paid, used, want int64
	}{{"unused", 10000, 0, 10000}, {"approved_partial", 10000, 20, 8000}, {"exhausted", 10000, 100, 0}, {"overused", 10000, 120, 0}, {"real_discount", 5000, 20, 4000}, {"half_up", 1, 50, 1}, {"free", 0, 20, 0}} {
		t.Run(test.name, func(t *testing.T) {
			s := perpetualValuePurchase()
			s.ActualPaidMinor = test.paid
			current := s.After
			current.FlowUsed = test.used
			for _, year := range []int{2020, 2060, 9998} {
				out, err := ProjectEntitlementValue([]EntitlementValueSnapshot{s}, current, "CNY", time.Date(year, 6, 1, 0, 0, 0, 0, time.UTC))
				if err != nil {
					t.Fatal(err)
				}
				assertProjectionConserves(t, out, test.want)
				if out.FutureMinor != 0 || out.ValuationModel != PerpetualQuotaAllocation {
					t.Fatal(out)
				}
			}
		})
	}
}

func TestPerpetualQuotaValueTrustedOldSnapshotAndRejectsUnknownHistory(t *testing.T) {
	s := perpetualValuePurchase()
	s.AllocationModel = "value_requires_review"
	current := s.After
	current.FlowUsed = 20
	at := s.TermStartAt.AddDate(5, 0, 0)
	out, err := ProjectEntitlementValue([]EntitlementValueSnapshot{s}, current, "CNY", at)
	if err != nil {
		t.Fatal(err)
	}
	assertProjectionConserves(t, out, 8000)
	for _, name := range []string{"unverified", "missing_policy", "legacy_credit", "reset", "periods", "unit", "additional_fact"} {
		t.Run(name, func(t *testing.T) {
			fact, state := s, current
			facts := []EntitlementValueSnapshot{fact}
			switch name {
			case "unverified":
				fact.PaymentVerified = false
			case "missing_policy":
				fact.AllocationModel = "purchase_value_policy_unavailable"
			case "legacy_credit":
				fact.LegacyTransferUnproven = true
			case "reset":
				state.CycleStartUsed = 20
			case "periods":
				fact.PaidPeriods = []PaidValuePeriod{{StartAt: fact.TermStartAt, EndAt: fact.TermEndAt, AmountMinor: 10000}}
			case "unit":
				fact.BillingUnit = "year"
			case "additional_fact":
				extra := fact
				extra.Mode, extra.QuotaEventID, extra.OrderID = "extended_term", 2, 2
				facts = append(facts, extra)
			}
			facts[0] = fact
			if _, err := ProjectEntitlementValue(facts, state, "CNY", at); err == nil {
				t.Fatal("unproven value accepted")
			}
		})
	}
}
