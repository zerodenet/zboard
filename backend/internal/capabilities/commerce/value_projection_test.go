package commerce

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"testing"
	"time"
)

func projectionPurchase(t *testing.T, start time.Time, unit string, count int, paid int64, policy int16) EntitlementValueSnapshot {
	t.Helper()
	end, err := entitlements.AddBillingPeriod(start, unit, count)
	if err != nil {
		t.Fatal(err)
	}
	periods, err := UniformPaidPeriods(start, end, unit, count, paid)
	if err != nil {
		t.Fatal(err)
	}
	return EntitlementValueSnapshot{Version: 1, OrderID: 1, SubscriptionID: 7, QuotaEventID: 1, QuotaEventAt: start, Currency: "CNY", ActualPaidMinor: paid, PaymentVerified: true, PaymentBasis: "verified_cash_or_wallet", Kind: "new", Mode: "new_term", CapturedAt: start, TermStartAt: start, TermEndAt: end, BillingUnit: unit, BillingValue: count, AllocationModel: "uniform_paid_units_v1", PaidPeriods: periods, After: EntitlementValueState{PlanID: 1, PlanSKUID: 1, StartAt: start, EndAt: end, Status: "active", FlowTotal: 100, ResetQuotaBytes: 100, ResetPolicy: policy, NextResetAt: entitlements.NextTrafficReset(start, policy)}}
}
func assertProjectionConserves(t *testing.T, out ValueProjection, want int64) {
	t.Helper()
	var sum int64
	for _, value := range out.Sources {
		sum += value
	}
	if out.TotalMinor != want || sum != want || out.CurrentMinor+out.FutureMinor != want || out.Fingerprint == "" {
		t.Fatalf("source budgets or total differ: %+v sum=%d want=%d", out, sum, want)
	}
}
func TestValueProjectionActualAnnualExamplesAndSources(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	root := projectionPurchase(t, start, "year", 1, 12000, 2)
	state := root.After
	state.FlowUsed = 300
	state.CycleStartUsed = 300
	state.FlowTotal = 400
	state.NextResetAt = entitlements.NextTrafficResetAfter(start, 2, start.AddDate(0, 3, 0))
	out, err := ProjectEntitlementValue([]EntitlementValueSnapshot{root}, state, "CNY", start.AddDate(0, 3, 0))
	if err != nil {
		t.Fatal(err)
	}
	assertProjectionConserves(t, out, 9000)
	state.FlowUsed = 280
	state.CycleStartUsed = 200
	state.FlowTotal = 300
	state.NextResetAt = entitlements.NextTrafficResetAfter(start, 2, start.AddDate(0, 2, 0))
	out, err = ProjectEntitlementValue([]EntitlementValueSnapshot{root}, state, "CNY", start.AddDate(0, 2, 0))
	if err != nil {
		t.Fatal(err)
	}
	assertProjectionConserves(t, out, 9200)
	if out.CurrentMinor != 200 || out.FutureMinor != 9000 {
		t.Fatal(out)
	}
	shared := projectionPurchase(t, start, "year", 1, 12000, 0)
	state = shared.After
	state.FlowUsed = 80
	out, err = ProjectEntitlementValue([]EntitlementValueSnapshot{shared}, state, "CNY", start.AddDate(0, 3, 0))
	if err != nil {
		t.Fatal(err)
	}
	assertProjectionConserves(t, out, 2400)
	if out.FutureMinor != 0 {
		t.Fatal("shared annual quota gained future months")
	}
}
func resetProjectionFact(root EntitlementValueSnapshot, before EntitlementValueState, id, event uint, at time.Time, paid int64) EntitlementValueSnapshot {
	after := before
	after.CycleStartUsed = before.FlowUsed
	after.FlowTotal = before.FlowUsed + before.ResetQuotaBytes
	return EntitlementValueSnapshot{Version: 1, OrderID: id, SubscriptionID: root.SubscriptionID, QuotaEventID: event, QuotaEventAt: at, Currency: "CNY", ActualPaidMinor: paid, PaymentVerified: true, PaymentBasis: "verified_cash_or_wallet", Kind: "traffic_reset", Mode: "paid_reset", CapturedAt: at, TermStartAt: at, TermEndAt: before.EndAt, AllocationModel: "reset_value_continuity_v1", Before: &before, After: after}
}
func TestValueProjectionMultiplePaidResetsDoNotReviveConsumedCash(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	root := projectionPurchase(t, start, "month", 1, 1000, 2)
	before := root.After
	before.FlowUsed = 80
	at := start.AddDate(0, 0, 10)
	one := resetProjectionFact(root, before, 2, 2, at, 100)
	out, err := ProjectEntitlementValue([]EntitlementValueSnapshot{root, one}, one.After, "CNY", at)
	if err != nil {
		t.Fatal(err)
	}
	assertProjectionConserves(t, out, 300)
	if out.Sources[1] != 200 || out.Sources[2] != 100 {
		t.Fatal(out.Sources)
	}
	two := resetProjectionFact(root, one.After, 3, 3, at, 100)
	// Deliberately unordered input cannot change serialized entitlement history.
	records := []EntitlementValueSnapshot{two, root, one}
	out, err = ProjectEntitlementValue(records, two.After, "CNY", at)
	if err != nil {
		t.Fatal(err)
	}
	assertProjectionConserves(t, out, 400)
	consumed := two.After
	consumed.FlowUsed = 130
	later := at.AddDate(0, 0, 10)
	out, err = ProjectEntitlementValue(records, consumed, "CNY", later)
	if err != nil {
		t.Fatal(err)
	}
	assertProjectionConserves(t, out, 200)
	three := resetProjectionFact(root, consumed, 4, 4, later, 100)
	records = append(records, three)
	out, err = ProjectEntitlementValue(records, three.After, "CNY", later)
	if err != nil {
		t.Fatal(err)
	}
	assertProjectionConserves(t, out, 300)
	if out.Sources[1] != 100 || out.Sources[2] != 50 || out.Sources[3] != 50 || out.Sources[4] != 100 {
		t.Fatal(out.Sources)
	}
	records = append(records, three)
	if _, err := ProjectEntitlementValue(records, three.After, "CNY", later); err == nil {
		t.Fatal("duplicate value fact doubled funds")
	}
}
func TestValueProjectionUnverifiedFactsAndCurrencyNeverBecomeZeroQuotes(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	root := projectionPurchase(t, start, "month", 1, 1000, 2)
	if _, err := ProjectEntitlementValue([]EntitlementValueSnapshot{root}, root.After, "USD", start); err == nil {
		t.Fatal("currency conversion invented")
	}
	root.PaymentVerified = false
	if _, err := ProjectEntitlementValue([]EntitlementValueSnapshot{root}, root.After, "CNY", start); !errors.Is(err, ErrValueEvidence) {
		t.Fatalf("unverified cash produced a quote: %v", err)
	}
	if _, err := ProjectEntitlementValue(nil, root.After, "CNY", start); !errors.Is(err, ErrValueEvidence) {
		t.Fatal("missing history became free value")
	}
}

func TestValueProjectionFutureRenewalIsPreservedAndCurrentOverlapRequiresAgreement(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	root := projectionPurchase(t, start, "month", 1, 1000, 1)
	before := root.After
	after := before
	after.EndAt = start.AddDate(0, 2, 0)
	renewal := root
	renewal.OrderID = 2
	renewal.QuotaEventID = 2
	renewal.QuotaEventAt = start.AddDate(0, 0, 15)
	renewal.CapturedAt = renewal.QuotaEventAt
	renewal.Mode = "extended_term"
	renewal.Kind = "renewal"
	renewal.Before = &before
	renewal.After = after
	renewal.TermStartAt = before.EndAt
	renewal.TermEndAt = after.EndAt
	var err error
	renewal.PaidPeriods, err = UniformPaidPeriods(renewal.TermStartAt, renewal.TermEndAt, "month", 1, 1000)
	if err != nil {
		t.Fatal(err)
	}
	out, err := ProjectEntitlementValue([]EntitlementValueSnapshot{root, renewal}, after, "CNY", renewal.QuotaEventAt)
	if err != nil || out.FutureMinor != 1000 || out.TotalMinor != out.CurrentMinor+1000 || out.Sources[2] != 1000 {
		t.Fatalf("future-only renewal repriced %+v %v", out, err)
	}
	// A subscription bought mid-calendar-month ends inside a later quota cycle.
	// Extending it while that last cycle is in progress adds value to the same
	// quota cycle. The separate user decision must be implemented before quoting.
	start = time.Date(2026, 1, 19, 0, 0, 0, 0, time.UTC)
	root = projectionPurchase(t, start, "month", 1, 1000, 1)
	before = root.After
	before.FlowUsed = 80
	before.NextResetAt = entitlements.NextTrafficResetAfter(start, 1, time.Date(2026, 2, 5, 0, 0, 0, 0, time.UTC))
	after = before
	after.EndAt = start.AddDate(0, 2, 0)
	renewal.OrderID = 2
	renewal.QuotaEventAt = time.Date(2026, 2, 5, 0, 0, 0, 0, time.UTC)
	renewal.CapturedAt = renewal.QuotaEventAt
	renewal.Before = &before
	renewal.After = after
	renewal.TermStartAt = before.EndAt
	renewal.TermEndAt = after.EndAt
	renewal.PaidPeriods, err = UniformPaidPeriods(renewal.TermStartAt, renewal.TermEndAt, "month", 1, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ProjectEntitlementValue([]EntitlementValueSnapshot{root, renewal}, after, "CNY", renewal.QuotaEventAt); !errors.Is(err, ErrValueRenewalOverlap) {
		t.Fatalf("unapproved current-cycle renewal value %v", err)
	}
}
