package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func perpetualBaseSyncFixture(t *testing.T, used, newBase int64) ([]EntitlementValueSnapshot, PerpetualBaseQuotaSyncEvent) {
	t.Helper()
	root := perpetualValuePurchase()
	add := perpetualAddition(root, 2, 0, 100, 10000)
	records := []EntitlementValueSnapshot{root, add}
	current := add.After
	current.FlowUsed = used
	at := add.CapturedAt.Add(time.Hour)
	detail, err := PreparePerpetualBaseQuotaSyncContext(t.Context(), records, nil, current, "CNY", at, 7, newBase, 10)
	if err != nil {
		t.Fatal(err)
	}
	return records, PerpetualBaseQuotaSyncEvent{QuotaEventID: 3, QuotaEventAt: at, Detail: detail}
}
func projectBaseSync(t *testing.T, records []EntitlementValueSnapshot, events []PerpetualBaseQuotaSyncEvent, current EntitlementValueState) ValueProjection {
	t.Helper()
	out, err := ProjectEntitlementValueWithBaseSyncContext(t.Context(), records, events, current, "CNY", events[len(events)-1].QuotaEventAt.Add(time.Hour), 7)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func assertBaseSyncSources(t *testing.T, out ValueProjection, want map[uint]int64) {
	t.Helper()
	var total int64
	for _, value := range want {
		total += value
	}
	assertProjectionConserves(t, out, total)
	if !reflect.DeepEqual(out.Sources, want) || out.ValuationModel != PerpetualBaseQuotaSyncAllocation || out.FutureMinor != 0 || out.Fingerprint == "" {
		t.Fatalf("source attribution changed: %+v sources=%v", out, out.Sources)
	}
}

func TestPerpetualBaseSyncApprovedGrowthChangesOnlyBase(t *testing.T) {
	records, event := perpetualBaseSyncFixture(t, 60, 200)
	if event.Detail.After.FlowTotal != 300 || event.Detail.After.FlowUsed != 60 || event.Detail.BaseConsumedBytes != 60 || event.Detail.OldBaseQuota != 100 || event.Detail.BaseRemainingAfter != 140 || event.Detail.After.ResetQuotaBytes != 200 {
		t.Fatal(event.Detail)
	}
	before, _ := json.Marshal(records)
	out := projectBaseSync(t, records, []PerpetualBaseQuotaSyncEvent{event}, event.Detail.After)
	assertBaseSyncSources(t, out, map[uint]int64{1: 7000, 2: 10000})
	after, _ := json.Marshal(records)
	if string(before) != string(after) {
		t.Fatal("sync mutated an original purchase/payment snapshot")
	}
	again := projectBaseSync(t, records, []PerpetualBaseQuotaSyncEvent{event}, event.Detail.After)
	if !reflect.DeepEqual(out, again) {
		t.Fatal("repeated pure valuation changed financial identity")
	}
}

func TestPerpetualBaseSyncNeverMovesPastAddonConsumptionBackToBase(t *testing.T) {
	records, event := perpetualBaseSyncFixture(t, 150, 200)
	if event.Detail.BaseConsumedBytes != 100 || event.Detail.BaseRemainingBefore != 0 || event.Detail.BaseRemainingAfter != 100 || event.Detail.After.FlowTotal != 300 {
		t.Fatal(event.Detail)
	}
	for _, tc := range []struct{ used, base, addon int64 }{{150, 5000, 5000}, {175, 3750, 5000}, {250, 0, 5000}, {275, 0, 2500}, {300, 0, 0}} {
		current := event.Detail.After
		current.FlowUsed = tc.used
		out := projectBaseSync(t, records, []PerpetualBaseQuotaSyncEvent{event}, current)
		sources := map[uint]int64{}
		if tc.base > 0 {
			sources[1] = tc.base
		}
		if tc.addon > 0 {
			sources[2] = tc.addon
		}
		assertBaseSyncSources(t, out, sources)
	}
}

func TestPerpetualBaseSyncDownBelowConsumedPreservesAddonRightsAndReexpands(t *testing.T) {
	for _, used := range []int64{60, 150} {
		records, event := perpetualBaseSyncFixture(t, used, 50)
		remaining := int64(100)
		if used == 150 {
			remaining = 50
		}
		if event.Detail.After.FlowTotal != used+remaining || event.Detail.After.FlowUsed != used || event.Detail.BaseRemainingAfter != 0 {
			t.Fatal("downsync swallowed separately bought quota", event.Detail)
		}
		before := event.Detail.After
		if used == 60 {
			before.FlowUsed = 159
		} // 99 add-on bytes really consumed after shrink.
		expanded, err := PreparePerpetualBaseQuotaSyncContext(t.Context(), records, []PerpetualBaseQuotaSyncEvent{event}, before, "CNY", event.QuotaEventAt.Add(time.Hour), 7, 200, 11)
		if err != nil {
			t.Fatal(err)
		}
		next := PerpetualBaseQuotaSyncEvent{QuotaEventID: 4, QuotaEventAt: event.QuotaEventAt.Add(time.Hour), Detail: expanded}
		out := projectBaseSync(t, records, []PerpetualBaseQuotaSyncEvent{event, next}, expanded.After)
		if used == 60 {
			assertBaseSyncSources(t, out, map[uint]int64{1: 7000, 2: 100})
		} else {
			assertBaseSyncSources(t, out, map[uint]int64{1: 5000, 2: 5000})
		}
	}
}

func TestPerpetualBaseSyncLaterRenewalDoesNotBecomeTheBaseLot(t *testing.T) {
	records, event := perpetualBaseSyncFixture(t, 150, 200)
	before := event.Detail.After
	after := before
	after.FlowTotal += 75
	after.ResetQuotaBytes = 75 // The ordinary renewal field cannot identify base quota.
	at := event.QuotaEventAt.Add(time.Hour)
	third := perpetualAddition(records[1], 4, 150, 75, 3000)
	third.Before, third.After, third.QuotaEventID, third.QuotaEventAt, third.CapturedAt, third.TermStartAt = &before, after, 4, at, at, at
	records = append(records, third)
	current := after
	current.FlowUsed = 190
	detail, err := PreparePerpetualBaseQuotaSyncContext(t.Context(), records, []PerpetualBaseQuotaSyncEvent{event}, current, "CNY", at.Add(time.Hour), 7, 250, 11)
	if err != nil {
		t.Fatal(err)
	}
	if detail.BaseOrderID != 1 || detail.OldBaseQuota != 200 || detail.After.FlowTotal != 425 || detail.BaseConsumedBytes != 140 {
		t.Fatal("latest batch was mistaken for original base", detail)
	}
	next := PerpetualBaseQuotaSyncEvent{QuotaEventID: 5, QuotaEventAt: at.Add(time.Hour), Detail: detail}
	out := projectBaseSync(t, records, []PerpetualBaseQuotaSyncEvent{event, next}, detail.After)
	assertBaseSyncSources(t, out, map[uint]int64{1: 4400, 2: 5000, 4: 3000})
}

func TestPerpetualBaseSyncKeepsActualDiscountedPaidCapsAndHalfUp(t *testing.T) {
	root := perpetualValuePurchase()
	root.ActualPaidMinor = 5000
	add := perpetualAddition(root, 2, 0, 100, 2000)
	current := add.After
	current.FlowUsed = 150
	at := add.CapturedAt.Add(time.Hour)
	detail, err := PreparePerpetualBaseQuotaSyncContext(t.Context(), []EntitlementValueSnapshot{root, add}, nil, current, "CNY", at, 7, 200, 10)
	if err != nil {
		t.Fatal(err)
	}
	event := PerpetualBaseQuotaSyncEvent{QuotaEventID: 3, QuotaEventAt: at, Detail: detail}
	assertBaseSyncSources(t, projectBaseSync(t, []EntitlementValueSnapshot{root, add}, []PerpetualBaseQuotaSyncEvent{event}, detail.After), map[uint]int64{1: 2500, 2: 1000})
	root = perpetualValuePurchase()
	root.After.FlowTotal, root.After.ResetQuotaBytes, root.ActualPaidMinor = 3, 3, 1
	add = perpetualAddition(root, 2, 1, 2, 1)
	current = add.After
	detail, err = PreparePerpetualBaseQuotaSyncContext(t.Context(), []EntitlementValueSnapshot{root, add}, nil, current, "CNY", at, 7, 6, 10)
	if err != nil {
		t.Fatal(err)
	}
	event = PerpetualBaseQuotaSyncEvent{QuotaEventID: 3, QuotaEventAt: at, Detail: detail}
	assertBaseSyncSources(t, projectBaseSync(t, []EntitlementValueSnapshot{root, add}, []PerpetualBaseQuotaSyncEvent{event}, detail.After), map[uint]int64{1: 1, 2: 1})
	current = detail.After
	current.FlowUsed = 4
	assertBaseSyncSources(t, projectBaseSync(t, []EntitlementValueSnapshot{root, add}, []PerpetualBaseQuotaSyncEvent{event}, current), map[uint]int64{2: 1})
}

func TestPerpetualBaseSyncRejectsMissingMalformedUnorderedAndWrongAuthority(t *testing.T) {
	for _, name := range []string{"missing event", "wrong version", "wrong kind", "wrong root", "wrong subscription", "wrong epoch", "wrong plan", "zero revision", "wrong old quota", "wrong used", "wrong consumption", "wrong remainder", "wrong sources", "before counter", "after counter", "after addon loss", "unknown final adjustment", "reset", "original paid changed", "duplicate source id", "unknown legacy overrun"} {
		t.Run(name, func(t *testing.T) {
			records, event := perpetualBaseSyncFixture(t, 150, 200)
			current := event.Detail.After
			events := []PerpetualBaseQuotaSyncEvent{event}
			switch name {
			case "missing event":
				events = nil
			case "wrong version":
				events[0].Detail.Version++
			case "wrong kind":
				events[0].Detail.Kind = "traffic_reset"
			case "wrong root":
				events[0].Detail.BaseOrderID = 2
			case "wrong subscription":
				events[0].Detail.SubscriptionID++
			case "wrong epoch":
				events[0].Detail.EntitlementEpoch++
			case "wrong plan":
				events[0].Detail.PlanID++
			case "zero revision":
				events[0].Detail.PlanRevision = 0
			case "wrong old quota":
				events[0].Detail.OldBaseQuota++
			case "wrong used":
				events[0].Detail.UsedAtSync++
			case "wrong consumption":
				events[0].Detail.BaseConsumedBytes--
			case "wrong remainder":
				events[0].Detail.BaseRemainingAfter++
			case "wrong sources":
				events[0].Detail.SourceFingerprint = strings.Repeat("0", 64)
			case "before counter":
				events[0].Detail.Before.FlowTotal++
			case "after counter":
				events[0].Detail.After.FlowUsed++
			case "after addon loss":
				events[0].Detail.After.FlowTotal = 250
			case "unknown final adjustment":
				current.FlowTotal++
			case "reset":
				records[1].Kind = "traffic_reset"
			case "original paid changed":
				records[0].ActualPaidMinor++
			case "duplicate source id":
				events[0].QuotaEventID = records[1].QuotaEventID
			case "unknown legacy overrun":
				records[1] = perpetualAddition(records[0], 2, 120, 100, 10000)
			}
			if _, err := ProjectEntitlementValueWithBaseSyncContext(t.Context(), records, events, current, "CNY", event.QuotaEventAt.Add(time.Hour), 7); err == nil {
				t.Fatal("unproved sync produced refundable value")
			}
		})
	}
	records, event := perpetualBaseSyncFixture(t, 150, 200)
	detail, err := PreparePerpetualBaseQuotaSyncContext(t.Context(), records, []PerpetualBaseQuotaSyncEvent{event}, event.Detail.After, "CNY", event.QuotaEventAt.Add(time.Hour), 7, 250, 11)
	if err != nil {
		t.Fatal(err)
	}
	second := PerpetualBaseQuotaSyncEvent{QuotaEventID: 4, QuotaEventAt: event.QuotaEventAt.Add(time.Hour), Detail: detail}
	for _, events := range [][]PerpetualBaseQuotaSyncEvent{{second, event}, {event, event}, {second}} {
		if _, err := ProjectEntitlementValueWithBaseSyncContext(t.Context(), records, events, detail.After, "CNY", second.QuotaEventAt.Add(time.Hour), 7); err == nil {
			t.Fatal("missing, duplicate or reversed authority was silently repaired")
		}
	}
}

func TestPerpetualBaseSyncSameMillisecondUsesRealEventIDsAndChangesQuoteIdentity(t *testing.T) {
	records, event := perpetualBaseSyncFixture(t, 60, 200)
	event.QuotaEventAt = records[1].QuotaEventAt.Add(100 * time.Nanosecond).Truncate(time.Millisecond)
	out := projectBaseSync(t, records, []PerpetualBaseQuotaSyncEvent{event}, event.Detail.After)
	changed := event
	changed.QuotaEventID = 30
	other := projectBaseSync(t, records, []PerpetualBaseQuotaSyncEvent{changed}, changed.Detail.After)
	if out.TotalMinor != other.TotalMinor || out.Fingerprint == other.Fingerprint {
		t.Fatal("actual source identity did not bind quote")
	}
	// DB rounding can put a prior row just after a later JSON timestamp. This
	// known precision loss cannot override increasing true event IDs/state proof.
	event.QuotaEventAt = records[1].QuotaEventAt.Add(time.Microsecond)
	detail, err := PreparePerpetualBaseQuotaSyncContext(t.Context(), records, []PerpetualBaseQuotaSyncEvent{event}, event.Detail.After, "CNY", records[1].QuotaEventAt.Add(time.Hour), 7, 250, 11)
	if err != nil {
		t.Fatal(err)
	}
	second := PerpetualBaseQuotaSyncEvent{QuotaEventID: 4, QuotaEventAt: event.QuotaEventAt.Add(-500 * time.Nanosecond), Detail: detail}
	_ = projectBaseSync(t, records, []PerpetualBaseQuotaSyncEvent{event, second}, detail.After)
	if _, err := ProjectEntitlementValueWithBaseSyncContext(t.Context(), records, []PerpetualBaseQuotaSyncEvent{event}, event.Detail.After, "CNY", event.QuotaEventAt.Add(time.Hour), 8); err == nil {
		t.Fatal("old epoch source revived in a new epoch")
	}
}

func TestPerpetualBaseSyncStrictDetailJSON(t *testing.T) {
	_, event := perpetualBaseSyncFixture(t, 60, 200)
	valid, err := json.Marshal(event.Detail)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePerpetualBaseQuotaSyncDetail(valid); err != nil {
		t.Fatal("valid zero-bearing epoch/state rejected", err)
	}
	for _, name := range []string{"unknown", "missing epoch", "missing nested used", "null state", "duplicate top", "duplicate nested", "trailing", "case alias"} {
		t.Run(name, func(t *testing.T) {
			var payload map[string]json.RawMessage
			_ = json.Unmarshal(valid, &payload)
			switch name {
			case "unknown":
				payload["paid_minor"] = json.RawMessage("999999")
			case "missing epoch":
				delete(payload, "entitlement_epoch")
			case "missing nested used":
				var before map[string]json.RawMessage
				_ = json.Unmarshal(payload["before"], &before)
				delete(before, "flow_used")
				payload["before"], _ = json.Marshal(before)
			case "null state":
				payload["before"] = json.RawMessage("null")
			case "case alias":
				payload["Version"] = json.RawMessage("1")
			}
			data, _ := json.Marshal(payload)
			if name == "duplicate top" {
				data = []byte(strings.Replace(string(valid), `"version":1`, `"version":1,"version":1`, 1))
			}
			if name == "duplicate nested" {
				data = []byte(strings.Replace(string(valid), `"flow_used":60`, `"flow_used":60,"flow_used":60`, 1))
			}
			if name == "trailing" {
				data = append(data, []byte(` {}`)...)
			}
			if _, err := DecodePerpetualBaseQuotaSyncDetail(data); err == nil {
				t.Fatal("malformed authority was accepted", string(data))
			}
		})
	}
}

func TestPerpetualBaseSyncOverflowAndCancellationNeverReturnQuote(t *testing.T) {
	records, event := perpetualBaseSyncFixture(t, 60, 200)
	current := records[1].After
	current.FlowUsed = 60
	if _, err := PreparePerpetualBaseQuotaSyncContext(t.Context(), records, nil, current, "CNY", event.QuotaEventAt, 7, math.MaxInt64, 10); err == nil {
		t.Fatal("aggregate quota overflow accepted")
	}
	records[0].ActualPaidMinor = math.MaxInt64
	records[1].ActualPaidMinor = 1
	if _, err := PreparePerpetualBaseQuotaSyncContext(t.Context(), records, nil, current, "CNY", event.QuotaEventAt, 7, 200, 10); err == nil {
		t.Fatal("paid-source overflow accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	out, err := ProjectEntitlementValueWithBaseSyncContext(ctx, records, []PerpetualBaseQuotaSyncEvent{event}, event.Detail.After, "CNY", event.QuotaEventAt.Add(time.Hour), 7)
	if !errors.Is(err, context.Canceled) || out.Fingerprint != "" {
		t.Fatal("cancellation returned a usable quote", out, err)
	}
}

func TestPerpetualBaseSyncEmptyHistoryPreservesExistingGoldenFingerprints(t *testing.T) {
	golden := map[string]string{
		"lots2/overrun0":   "bf93be402784630b02bcc1cc911651d289126938b55b4eee14bc330e8812384f",
		"lots2/overrun25":  "d32bbde4a6f92260d45576c7884e88b7d90859fd880185b08543030e20b46694",
		"lots17/overrun0":  "a865bdce3d5e6b3753d0ebd0c19ef900b45d29be0ccca6b07092de7a1be7343f",
		"lots17/overrun25": "551f1ab4e03d6c57f501207d0cb9e2e6ca10efe93ced014b2a01615f6c9a18b6",
	}
	for _, n := range []int{2, 17} {
		for _, overrun := range []int64{0, 25} {
			records, current, at := perpetualSequentialHistory(n, overrun)
			out, err := ProjectEntitlementValueWithBaseSyncContext(t.Context(), records, nil, current, "CNY", at, 999)
			if err != nil || out.Fingerprint != golden[fmt.Sprintf("lots%d/overrun%d", n, overrun)] {
				t.Fatal("old no-sync quote identity changed", out, err)
			}
		}
	}
}

func TestPerpetualBaseSyncInputOrderAndEpochAreBoundWithoutMutation(t *testing.T) {
	records, event := perpetualBaseSyncFixture(t, 60, 200)
	ordered := projectBaseSync(t, records, []PerpetualBaseQuotaSyncEvent{event}, event.Detail.After)
	reversed := []EntitlementValueSnapshot{records[1], records[0]}
	before, _ := json.Marshal(reversed)
	out := projectBaseSync(t, reversed, []PerpetualBaseQuotaSyncEvent{event}, event.Detail.After)
	after, _ := json.Marshal(reversed)
	if string(before) != string(after) || out.Fingerprint != ordered.Fingerprint {
		t.Fatal("caller input order was mutated or used instead of actual grant IDs")
	}
	current := records[1].After
	current.FlowUsed = 60
	zero, err := PreparePerpetualBaseQuotaSyncContext(t.Context(), records, nil, current, "CNY", event.QuotaEventAt, 0, 200, 10)
	if err != nil {
		t.Fatal(err)
	}
	zeroEvent := PerpetualBaseQuotaSyncEvent{QuotaEventID: 3, QuotaEventAt: event.QuotaEventAt, Detail: zero}
	zeroQuote, err := ProjectEntitlementValueWithBaseSyncContext(t.Context(), records, []PerpetualBaseQuotaSyncEvent{zeroEvent}, zero.After, "CNY", event.QuotaEventAt.Add(time.Hour), 0)
	if err != nil || zeroQuote.TotalMinor != ordered.TotalMinor || zeroQuote.Fingerprint == ordered.Fingerprint {
		t.Fatal("valid zero epoch was rejected or failed to bind quote", zeroQuote, err)
	}
}

func TestPerpetualBaseSyncLongProofChainRejectsMissingRoundTrip(t *testing.T) {
	root := perpetualValuePurchase()
	add := perpetualAddition(root, 2, 0, 100, 10000)
	records := []EntitlementValueSnapshot{root, add}
	current := add.After
	current.FlowUsed = 60
	var events []PerpetualBaseQuotaSyncEvent
	at := add.CapturedAt.Add(time.Hour)
	for i := 0; i < 100; i++ {
		moment := at.Add(time.Duration(i) * time.Millisecond)
		detail, err := PreparePerpetualBaseQuotaSyncContext(t.Context(), records, events, current, "CNY", moment, 7, int64(200+i%2), uint64(10+i))
		if err != nil {
			t.Fatal(i, err)
		}
		events = append(events, PerpetualBaseQuotaSyncEvent{QuotaEventID: uint(3 + i), QuotaEventAt: moment, Detail: detail})
		current = detail.After
	}
	out := projectBaseSync(t, records, events, current)
	assertBaseSyncSources(t, out, map[uint]int64{1: 7015, 2: 10000})
	// Removing a growth/shrink pair leaves the same counters/base quota. Only
	// complete immutable source provenance can detect that missing authority.
	missing := append([]PerpetualBaseQuotaSyncEvent(nil), events[:40]...)
	missing = append(missing, events[42:]...)
	if _, err := ProjectEntitlementValueWithBaseSyncContext(t.Context(), records, missing, current, "CNY", at.Add(time.Hour), 7); err == nil {
		t.Fatal("round-trip event omission escaped source proof")
	}
}
