package commerce

import (
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

// The existing MySQL RR regression commits a real task_adjustment quota event
// while replacement payment owns an earlier snapshot. It changes accepted
// rights, not the plan's base quota or rounded paid residual. With no plan-sync
// evidence the compatibility reader must keep the old projection/fingerprint;
// the current-event authority still rejects the stale accepted quote.
func TestTimedNoPlanSyncPreservesLegacyCurrentQuotaAdjustment(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := start.AddDate(0, 3, 0)
	root := projectionPurchase(t, start, "year", 1, 12000, 2)
	current := root.After
	current.NextResetAt = entitlements.NextTrafficResetAfter(start, current.ResetPolicy, at)
	current.FlowTotal++
	records := []EntitlementValueSnapshot{root}
	old, err := ProjectEntitlementValue(records, current, "CNY", at)
	if err != nil || old.TotalMinor != 9000 {
		t.Fatal("existing unchanged-money quota adjustment is not reproducible", old, err)
	}
	currentQuote, err := ProjectTimedEntitlementValueWithPlanSyncContext(t.Context(), records, nil, current, "CNY", at, 0)
	if err != nil || currentQuote.TotalMinor != old.TotalMinor || currentQuote.Fingerprint != old.Fingerprint {
		t.Fatal("no-plan-sync compatibility rejected accepted legacy quota adjustment", currentQuote, old, err)
	}
}

func TestTimedMissingPlanSyncBeforeLaterPaymentStillRejects(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	at := start.AddDate(0, 0, 5)
	root := projectionPurchase(t, start, "month", 1, 1000, 2)
	before := root.After
	before.FlowUsed = 60
	cap := timedSyncProof(t, []EntitlementValueSnapshot{root}, nil, before, at, 2, 200, 10)
	addon := timedAdditionFact(root, cap.Detail.After, 3, at, 2000, 100)
	records := []EntitlementValueSnapshot{root, addon}
	withEvidence := timedSyncValue(t, records, []TimedPlanQuotaSyncEvent{cap}, addon.After, at)
	assertProjectionConserves(t, withEvidence, 2400)
	// Matching the LAST paid snapshot's base is insufficient: that later
	// payment already observed the free resize. Missing its preceding cap may
	// not silently restore the original legacy2700 calculation.
	legacy, err := ProjectEntitlementValue(records, addon.After, "CNY", at)
	if err != nil || legacy.TotalMinor != 2700 {
		t.Fatal("missing-cap counterexample is not reproducible", legacy, err)
	}
	if quote, err := ProjectTimedEntitlementValueWithPlanSyncContext(t.Context(), records, nil, addon.After, "CNY", at, 7); err == nil {
		t.Fatal("later payment hid the missing free-resize value cap", quote)
	}
}

func TestTimedNoPlanSyncPreservesPaidFutureBaseChangeAndLegacyAdjustment(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	at := start.AddDate(0, 0, 5)
	root := projectionPurchase(t, start, "month", 1, 1000, 2)
	renewal := projectionRenewal(t, root, root.After, 2, at, 2000)
	// Early renewal can change the future base without resetting this cycle.
	renewal.After.ResetQuotaBytes = 200
	current := renewal.After
	current.FlowTotal++
	records := []EntitlementValueSnapshot{root, renewal}
	old, err := ProjectEntitlementValue(records, current, "CNY", at)
	if err != nil {
		t.Fatal("existing paid future-base transition is not reproducible", old, err)
	}
	actual, err := ProjectTimedEntitlementValueWithPlanSyncContext(t.Context(), records, nil, current, "CNY", at, 0)
	if err != nil || actual.TotalMinor != old.TotalMinor || actual.Fingerprint != old.Fingerprint {
		t.Fatal("verified paid base change lost the existing no-sync quote", actual, old, err)
	}
}
