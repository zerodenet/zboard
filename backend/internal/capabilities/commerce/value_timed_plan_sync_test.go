package commerce

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func timedSyncProof(t *testing.T, records []EntitlementValueSnapshot, prior []TimedPlanQuotaSyncEvent, current EntitlementValueState, at time.Time, id uint, quota int64, revision uint64) TimedPlanQuotaSyncEvent {
	t.Helper()
	d, err := PrepareTimedPlanQuotaSyncContext(t.Context(), records, prior, current, "CNY", at, 7, quota, revision)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeTimedPlanQuotaSyncDetail(raw)
	if err != nil {
		t.Fatal(err)
	}
	return TimedPlanQuotaSyncEvent{QuotaEventID: id, QuotaEventAt: at, EntitlementEpoch: 7, Detail: &decoded}
}
func timedSyncValue(t *testing.T, records []EntitlementValueSnapshot, events []TimedPlanQuotaSyncEvent, state EntitlementValueState, at time.Time) ValueProjection {
	t.Helper()
	out, err := ProjectTimedEntitlementValueWithPlanSyncContext(t.Context(), records, events, state, "CNY", at, 7)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestTimedFreePlanSyncApprovedMixedExamplePreservesSettledValue(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	at := start.AddDate(0, 0, 5)
	root := projectionPurchase(t, start, "month", 1, 1000, 2)
	before := root.After
	before.FlowUsed = 60
	add := timedAdditionFact(root, before, 2, at, 2000, 100)
	records := []EntitlementValueSnapshot{root, add}
	old := timedSyncValue(t, records, nil, add.After, at)
	assertProjectionConserves(t, old, 2400)
	event := timedSyncProof(t, records, nil, add.After, at, 3, 200, 10)
	d := event.Detail
	if d.OldBaseQuota != 100 || d.AddonQuota != 100 || d.After.FlowTotal != 300 || d.After.FlowUsed != 60 || d.CurrentMinorBefore != 2400 || d.CurrentMinorAfter != 2400 {
		t.Fatal("free base resize changed add-on, usage or restored cash", d)
	}
	out := timedSyncValue(t, records, []TimedPlanQuotaSyncEvent{event}, d.After, at)
	assertProjectionConserves(t, out, 2400)
	if out.Sources[1] != 400 || out.Sources[2] != 2000 || out.FutureMinor != 0 {
		t.Fatal(out)
	}
	used := d.After
	used.FlowUsed += 120
	out = timedSyncValue(t, records, []TimedPlanQuotaSyncEvent{event}, used, at)
	assertProjectionConserves(t, out, 1200)
	later := at.Add(d.Before.EndAt.Sub(at) / 2)
	out = timedSyncValue(t, records, []TimedPlanQuotaSyncEvent{event}, d.After, later)
	assertProjectionConserves(t, out, 1200)
	again := timedSyncProof(t, records, []TimedPlanQuotaSyncEvent{event}, used, at, 4, 300, 11)
	out = timedSyncValue(t, records, []TimedPlanQuotaSyncEvent{event, again}, again.Detail.After, at)
	assertProjectionConserves(t, out, 1200)
	if again.Detail.After.FlowTotal != 400 || again.Detail.AddonQuota != 100 {
		t.Fatal(again)
	}
}
func TestTimedFreePlanSyncSinglePaidBaseCannotReviveConsumedMoney(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	at := start.AddDate(0, 0, 5)
	root := projectionPurchase(t, start, "month", 1, 1000, 2)
	state := root.After
	state.FlowUsed = 60
	records := []EntitlementValueSnapshot{root}
	before := timedSyncValue(t, records, nil, state, at)
	original, err := ProjectEntitlementValue(records, state, "CNY", at)
	if err != nil || before.Fingerprint != original.Fingerprint {
		t.Fatal("unchanged source lost legacy quote identity", before, original, err)
	}
	expanded := timedSyncProof(t, records, nil, state, at, 2, 200, 10)
	out := timedSyncValue(t, records, []TimedPlanQuotaSyncEvent{expanded}, expanded.Detail.After, at)
	assertProjectionConserves(t, out, 400)
	lowered := timedSyncProof(t, records, []TimedPlanQuotaSyncEvent{expanded}, expanded.Detail.After, at, 3, 50, 11)
	out = timedSyncValue(t, records, []TimedPlanQuotaSyncEvent{expanded, lowered}, lowered.Detail.After, at)
	assertProjectionConserves(t, out, 0)
	if lowered.Detail.After.FlowUsed != 60 || lowered.Detail.After.FlowTotal != 50 {
		t.Fatal("quota downscale rewrote used", lowered)
	}
	if _, err := ProjectTimedEntitlementValueWithPlanSyncContext(t.Context(), records, nil, expanded.Detail.After, "CNY", at, 7); err == nil {
		t.Fatal("missing cap event silently revived original paid denominator")
	}
}
func TestTimedFreePlanSyncPreservesFuturePaidCyclesAndExpiresOnlyAddon(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	at := start.AddDate(0, 0, 5)
	root := projectionPurchase(t, start, "year", 1, 12000, 2)
	state := root.After
	state.FlowUsed = 60
	add := timedAdditionFact(root, state, 2, at, 2000, 100)
	records := []EntitlementValueSnapshot{root, add}
	event := timedSyncProof(t, records, nil, add.After, at, 3, 200, 10)
	out := timedSyncValue(t, records, []TimedPlanQuotaSyncEvent{event}, event.Detail.After, at)
	assertProjectionConserves(t, out, 13400)
	if out.CurrentMinor != 2400 || out.FutureMinor != 11000 {
		t.Fatal(out)
	}
	next := event.Detail.After
	due := *next.NextResetAt
	following := due.AddDate(0, 1, 0)
	next.CycleStartUsed = next.FlowUsed
	next.FlowTotal = next.FlowUsed + next.ResetQuotaBytes
	next.NextResetAt = &following
	out = timedSyncValue(t, records, []TimedPlanQuotaSyncEvent{event}, next, due)
	assertProjectionConserves(t, out, 11000)
	if out.CurrentMinor != 1000 || out.FutureMinor != 10000 || out.Sources[2] != 0 {
		t.Fatal("free sync repriced future cash or extended an add-on", out)
	}
}
func TestTimedFreePlanSyncPaidResetAndAdditionalCashRetainOriginalSources(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	at := start.AddDate(0, 0, 5)
	root := projectionPurchase(t, start, "month", 1, 1000, 2)
	state := root.After
	state.FlowUsed = 60
	add := timedAdditionFact(root, state, 2, at, 2000, 100)
	records := []EntitlementValueSnapshot{root, add}
	event := timedSyncProof(t, records, nil, add.After, at, 3, 200, 10)
	reset := resetProjectionFact(root, event.Detail.After, 4, 4, at, 100)
	out := timedSyncValue(t, append(records, reset), []TimedPlanQuotaSyncEvent{event}, reset.After, at)
	assertProjectionConserves(t, out, 2500)
	if out.Sources[1] != 400 || out.Sources[2] != 2000 || out.Sources[4] != 100 {
		t.Fatal(out)
	}
	second := timedAdditionFact(root, event.Detail.After, 4, at, 500, 100)
	out = timedSyncValue(t, append(records, second), []TimedPlanQuotaSyncEvent{event}, second.After, at)
	assertProjectionConserves(t, out, 2900)
	if out.Sources[4] != 500 {
		t.Fatal(out)
	}
}
func TestTimedFreePlanSyncLegacyAuditCompatibilityStopsAtNewTypedEvent(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	at := start.AddDate(0, 0, 5)
	root := projectionPurchase(t, start, "month", 1, 1000, 2)
	records := []EntitlementValueSnapshot{root}
	legacy := TimedPlanQuotaSyncEvent{QuotaEventID: 2, QuotaEventAt: at, EntitlementEpoch: 7, Legacy: &LegacyTimedPlanQuotaSync{PlanID: 1, PlanRevision: 9, ActorID: 1, PreviousFlowTotal: 100, FlowTotal: 200, FlowUsed: 60, PreviousResetQuota: 100, ResetQuota: 200}}
	state := root.After
	state.FlowUsed = 60
	state.FlowTotal = 200
	state.ResetQuotaBytes = 200
	old := timedSyncValue(t, records, []TimedPlanQuotaSyncEvent{legacy}, state, at)
	original, err := ProjectEntitlementValue(records, state, "CNY", at)
	if err != nil || old.TotalMinor != 700 || old.Fingerprint != original.Fingerprint {
		t.Fatal("legacy evidence was silently repriced", old, original, err)
	}
	typed := timedSyncProof(t, records, []TimedPlanQuotaSyncEvent{legacy}, state, at, 3, 300, 10)
	out := timedSyncValue(t, records, []TimedPlanQuotaSyncEvent{legacy, typed}, typed.Detail.After, at)
	assertProjectionConserves(t, out, 700)
	later := legacy
	later.QuotaEventID = 4
	later.Legacy = &LegacyTimedPlanQuotaSync{PlanID: 1, PlanRevision: 11, ActorID: 1, PreviousFlowTotal: 300, FlowTotal: 400, FlowUsed: 60, PreviousResetQuota: 300, ResetQuota: 400}
	if _, err := ProjectTimedEntitlementValueWithPlanSyncContext(t.Context(), records, []TimedPlanQuotaSyncEvent{legacy, typed, later}, typed.Detail.After, "CNY", at, 7); err == nil {
		t.Fatal("legacy writer bypassed the new settled-value cap")
	}
}
func TestTimedFreePlanSyncRejectsChangedOrIncompleteAuthority(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	at := start.AddDate(0, 0, 5)
	root := projectionPurchase(t, start, "month", 1, 1000, 2)
	state := root.After
	state.FlowUsed = 60
	records := []EntitlementValueSnapshot{root}
	event := timedSyncProof(t, records, nil, state, at, 2, 200, 10)
	for _, variant := range []string{"epoch", "time", "source", "after", "money", "addon", "revision", "duplicate", "missing"} {
		t.Run(variant, func(t *testing.T) {
			d := *event.Detail
			e := event
			e.Detail = &d
			events := []TimedPlanQuotaSyncEvent{e}
			switch variant {
			case "epoch":
				d.EntitlementEpoch++
			case "time":
				d.CapturedAt = d.CapturedAt.Add(2 * time.Millisecond)
			case "source":
				d.SourceFingerprint = strings.Repeat("0", 64)
			case "after":
				d.After.FlowTotal++
			case "money":
				d.CurrentMinorAfter++
			case "addon":
				d.AddonQuota++
			case "revision":
				d.PlanRevision = 0
			case "duplicate":
				events = append(events, e)
			case "missing":
				events = nil
			}
			if _, err := ProjectTimedEntitlementValueWithPlanSyncContext(t.Context(), records, events, event.Detail.After, "CNY", at, 7); err == nil {
				t.Fatal("changed financial source was accepted")
			}
		})
	}
	raw, err := json.Marshal(event.Detail)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{strings.Replace(string(raw), `"version":1,`, ``, 1), strings.Replace(string(raw), `"version":1,`, `"version":1,"version":1,`, 1), strings.TrimSuffix(string(raw), "}") + `,"unknown":true}`, string(raw) + ` {}`} {
		if _, err := DecodeTimedPlanQuotaSyncDetail([]byte(value)); err == nil {
			t.Fatal("ambiguous detail accepted", value)
		}
	}
}

func TestTimedFreePlanSyncUsesCapturedClockAcrossMillisecondRepresentations(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	clock := start.AddDate(0, 0, 5)
	at := clock.Add(500 * time.Microsecond)
	root := projectionPurchase(t, start, "month", 1, 1000, 2)
	state := root.After
	state.FlowUsed = 60
	records := []EntitlementValueSnapshot{root}
	event := timedSyncProof(t, records, nil, state, at, 2, 200, 10)
	event.QuotaEventAt = clock
	paid := timedAdditionFact(root, event.Detail.After, 3, clock, 100, 100)
	paid.CapturedAt = clock.Add(800 * time.Microsecond)
	paid.TermStartAt = paid.CapturedAt
	out := timedSyncValue(t, append(records, paid), []TimedPlanQuotaSyncEvent{event}, paid.After, paid.CapturedAt)
	assertProjectionConserves(t, out, 500)
}
func TestTimedFreePlanSyncKeepsOriginalUnixSecondBasisGuard(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 300000000, time.UTC)
	root := projectionPurchase(t, start, "month", 1, 1000, 2)
	at := root.After.EndAt.Add(-100 * time.Millisecond)
	if _, err := PrepareTimedPlanQuotaSyncContext(t.Context(), []EntitlementValueSnapshot{root}, nil, root.After, "CNY", at, 7, 200, 10); err == nil {
		t.Fatal("zero-second basis was stored despite the existing billing clock boundary")
	}
}
func TestTimedFreePlanSyncLegacyAuditAfterNaturalResetKeepsOldIdentity(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	at := start.AddDate(0, 1, 5)
	root := projectionPurchase(t, start, "year", 1, 12000, 2)
	records := []EntitlementValueSnapshot{root}
	event := TimedPlanQuotaSyncEvent{QuotaEventID: 2, QuotaEventAt: at, EntitlementEpoch: 7, Legacy: &LegacyTimedPlanQuotaSync{PlanID: 1, PlanRevision: 9, ActorID: 1, PreviousFlowTotal: 200, FlowTotal: 300, FlowUsed: 160, PreviousResetQuota: 100, ResetQuota: 200}}
	state := root.After
	state.FlowTotal = 300
	state.FlowUsed = 160
	state.CycleStartUsed = 100
	state.ResetQuotaBytes = 200
	next := start.AddDate(0, 2, 0)
	state.NextResetAt = &next
	out := timedSyncValue(t, records, []TimedPlanQuotaSyncEvent{event}, state, at)
	original, err := ProjectEntitlementValue(records, state, "CNY", at)
	if err != nil || out.TotalMinor != 10700 || out.Fingerprint != original.Fingerprint {
		t.Fatal("legacy reset/sync history repriced", out, original, err)
	}
}

func TestTimedFreePlanSyncCancellationNeverReturnsAQuote(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	root := projectionPurchase(t, start, "month", 1, 1000, 2)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	out, err := ProjectTimedEntitlementValueWithPlanSyncContext(ctx, []EntitlementValueSnapshot{root}, nil, root.After, "CNY", start, 7)
	if err != context.Canceled || out.Fingerprint != "" {
		t.Fatal("canceled replay returned a quote", out, err)
	}
}

func TestTimedFreePlanSyncCanonicalExpiryKeepsZeroValueWithoutRevivingRights(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	root := projectionPurchase(t, start, "year", 1, 12000, 2)
	at := start.AddDate(0, 0, 5)
	state := root.After
	state.FlowUsed = 60
	records := []EntitlementValueSnapshot{root}
	typed := timedSyncProof(t, records, nil, state, at, 2, 200, 10)
	for _, withSync := range []bool{false, true} {
		t.Run(map[bool]string{false: "original", true: "typed"}[withSync], func(t *testing.T) {
			current := state
			var events []TimedPlanQuotaSyncEvent
			if withSync {
				current = typed.Detail.After
				events = []TimedPlanQuotaSyncEvent{typed}
			}
			ended := current.EndAt
			current.Status = "expired"
			current.EndedAt = &ended
			current.EndReason = "expired"
			now := ended.Add(time.Hour)
			out := timedSyncValue(t, records, events, current, now)
			assertProjectionConserves(t, out, 0)
			if !withSync {
				old, err := ProjectEntitlementValue(records, current, "CNY", now)
				if err != nil || old.Fingerprint != out.Fingerprint {
					t.Fatal("canonical old zero quote identity changed", out, old, err)
				}
			}
			if current.Status != "expired" || current.EndedAt == nil || current.EndReason != "expired" {
				t.Fatal("read changed terminal business state", current)
			}
			if _, err := PrepareTimedPlanQuotaSyncContext(t.Context(), records, events, current, "CNY", now, 7, 300, 11); err == nil {
				t.Fatal("zero expired quote permitted a new entitlement mutation")
			}
			for _, reason := range []string{"canceled", "changed", "exhausted"} {
				bad := current
				bad.EndReason = reason
				if _, err := ProjectTimedEntitlementValueWithPlanSyncContext(t.Context(), records, events, bad, "CNY", now, 7); err == nil {
					t.Fatal("non-expiry terminal state regained valuation", reason)
				}
			}
			if withSync {
				if _, err := ProjectTimedEntitlementValueWithPlanSyncContext(t.Context(), records, nil, current, "CNY", now, 7); err == nil {
					t.Fatal("expiry hid a missing cap event")
				}
			}
		})
	}
}
func TestTimedFreePlanSyncZeroLegacyResetQuotaRetainsOriginalValueAndIdentity(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	at := start.AddDate(0, 0, 5)
	for _, quota := range []int64{0, 100} {
		t.Run(map[int64]string{0: "zero-quota", 100: "known-quota"}[quota], func(t *testing.T) {
			root := projectionPurchase(t, start, "month", 1, 1000, 2)
			root.After.FlowTotal = quota
			root.After.ResetQuotaBytes = 0
			state := root.After
			if quota > 0 {
				state.FlowUsed = 60
			}
			records := []EntitlementValueSnapshot{root}
			old, err := ProjectEntitlementValue(records, state, "CNY", at)
			if err != nil {
				t.Fatal(err)
			}
			out := timedSyncValue(t, records, nil, state, at)
			if out.TotalMinor != old.TotalMinor || out.Fingerprint != old.Fingerprint {
				t.Fatal("missing historical RQB became a new read eligibility rule", out, old)
			}
			event := timedSyncProof(t, records, nil, state, at, 2, 200, 10)
			after := timedSyncValue(t, records, []TimedPlanQuotaSyncEvent{event}, event.Detail.After, at)
			if after.TotalMinor > old.TotalMinor {
				t.Fatal("legacy zero RQB minted value", after, old)
			}
			if _, err := ProjectTimedEntitlementValueWithPlanSyncContext(t.Context(), records, nil, event.Detail.After, "CNY", at, 7); err == nil {
				t.Fatal("legacy zero RQB let a new cap disappear")
			}
		})
	}
	root := projectionPurchase(t, start, "year", 1, 12000, 2)
	root.After.ResetQuotaBytes = 0
	state := root.After
	state.FlowUsed = 160
	state.CycleStartUsed = 100
	state.FlowTotal = 200
	next := start.AddDate(0, 2, 0)
	state.NextResetAt = &next
	now := start.AddDate(0, 1, 5)
	original, err := ProjectEntitlementValue([]EntitlementValueSnapshot{root}, state, "CNY", now)
	if err != nil {
		t.Fatal(err)
	}
	out := timedSyncValue(t, []EntitlementValueSnapshot{root}, nil, state, now)
	if out.Fingerprint != original.Fingerprint || out.TotalMinor != 10400 {
		t.Fatal("proved natural cycle with zero legacy RQB changed", out, original)
	}
}

// These states reproduce a stopped reset/expiry worker. Before this fix strict
// finish checked the final calendar cycle and rejected both real histories,
// while the original projector correctly returned zero at term end.
func TestTimedFreePlanSyncLazyAndPartiallyResetExpiryRemainZero(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	root := projectionPurchase(t, start, "year", 1, 12000, 2)
	records := []EntitlementValueSnapshot{root}
	before := root.After
	before.FlowUsed = 60
	eventAt := start.AddDate(0, 0, 5)
	event := timedSyncProof(t, records, nil, before, eventAt, 2, 200, 10)
	now := root.After.EndAt.Add(time.Hour)
	for _, typed := range []bool{false, true} {
		for _, partial := range []bool{false, true} {
			for _, closed := range []bool{false, true} {
				name := map[bool]string{false: "original", true: "typed"}[typed] + "/" + map[bool]string{false: "never-reset", true: "one-reset"}[partial] + "/" + map[bool]string{false: "worker-pending", true: "canonical-ended"}[closed]
				t.Run(name, func(t *testing.T) {
					current := before
					var events []TimedPlanQuotaSyncEvent
					if typed {
						current = event.Detail.After
						events = []TimedPlanQuotaSyncEvent{event}
					}
					if partial {
						current.CycleStartUsed = 100
						current.FlowUsed = 160
						current.FlowTotal = current.CycleStartUsed + current.ResetQuotaBytes
						next := start.AddDate(0, 2, 0)
						current.NextResetAt = &next
					}
					if closed {
						ended := current.EndAt
						current.Status = "expired"
						current.EndedAt = &ended
						current.EndReason = "expired"
					}
					snapshot := current
					out := timedSyncValue(t, records, events, current, now)
					assertProjectionConserves(t, out, 0)
					if !closed {
						pending := current
						pending.Status = "expired"
						assertProjectionConserves(t, timedSyncValue(t, records, events, pending, now), 0)
					}
					if !typed {
						old, err := ProjectEntitlementValue(records, current, "CNY", now)
						if err != nil || out.Fingerprint != old.Fingerprint {
							t.Fatal("old expired quote identity changed", out, old, err)
						}
					}
					if current != snapshot {
						t.Fatal("zero read modified actual terminal or accounting state")
					}
					if _, err := PrepareTimedPlanQuotaSyncContext(t.Context(), records, events, current, "CNY", now, 7, 300, 11); err == nil {
						t.Fatal("zero expired read admitted new quota rights")
					}
					if typed {
						if _, err := ProjectTimedEntitlementValueWithPlanSyncContext(t.Context(), records, nil, current, "CNY", now, 7); err == nil {
							t.Fatal("stopped expiry hid an observable missing cap")
						}
					}
				})
			}
		}
	}
}
func TestTimedFreePlanSyncExpiredCycleProofStillRejectsUnexplainedChanges(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	root := projectionPurchase(t, start, "year", 1, 12000, 2)
	records := []EntitlementValueSnapshot{root}
	state := root.After
	state.FlowUsed = 160
	state.CycleStartUsed = 100
	state.FlowTotal = 200
	next := start.AddDate(0, 2, 0)
	state.NextResetAt = &next
	now := root.After.EndAt.Add(time.Hour)
	for _, variant := range []string{"quota", "rqb", "baseline", "schedule", "regressed-next", "closed-reason", "premature-end"} {
		t.Run(variant, func(t *testing.T) {
			bad := state
			switch variant {
			case "quota":
				bad.FlowTotal++
			case "rqb":
				bad.ResetQuotaBytes++
			case "baseline":
				bad.CycleStartUsed = bad.FlowUsed + 1
			case "schedule":
				badNext := next.Add(-time.Hour)
				bad.NextResetAt = &badNext
			case "regressed-next":
				badNext := start
				bad.NextResetAt = &badNext
			case "closed-reason":
				end := bad.EndAt
				bad.EndedAt = &end
				bad.Status = "expired"
				bad.EndReason = "canceled"
			case "premature-end":
				end := bad.EndAt.Add(-time.Hour)
				bad.EndedAt = &end
				bad.Status = "expired"
				bad.EndReason = "expired"
			}
			if _, err := ProjectTimedEntitlementValueWithPlanSyncContext(t.Context(), records, nil, bad, "CNY", now, 7); err == nil {
				t.Fatal("expired zero quote adopted unproved state", variant)
			}
		})
	}
	// While the term is live the same stale cycle is not silently catch-up priced.
	live := start.AddDate(0, 3, 0)
	if _, err := ProjectTimedEntitlementValueWithPlanSyncContext(t.Context(), records, nil, state, "CNY", live, 7); err == nil {
		t.Fatal("expired-only compatibility bypassed an unprocessed live reset")
	}
}
