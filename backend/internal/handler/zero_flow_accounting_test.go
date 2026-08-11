package handler

import (
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

func TestZeroFlowUsageKeyScopesReusedFlowIDByCoreInstance(t *testing.T) {
	first := zeroFlowUsageKey("core-a", "flow-7")
	second := zeroFlowUsageKey("core-b", "flow-7")
	if first == second {
		t.Fatalf("runtime flow keys must differ across core instances: %q", first)
	}
	if !zeroFlowUsageRuntimeScoped(first) || !zeroFlowUsageRuntimeScoped(second) {
		t.Fatalf("runtime flow keys must use the v2 namespace: %q %q", first, second)
	}
	if got := zeroFlowUsageKey("", "flow-7"); got != "flow-7" {
		t.Fatalf("legacy flow key = %q, want raw flow id", got)
	}
}

func TestPickZeroFlowUsagePrefersRuntimeScopedCursor(t *testing.T) {
	runtimeKey := zeroFlowUsageKey("core-a", "flow-1")
	legacy := model.FlowUsage{ID: 1, FlowID: "flow-1"}
	runtime := model.FlowUsage{ID: 2, FlowID: runtimeKey}

	got, found, isLegacy := pickZeroFlowUsage([]model.FlowUsage{legacy, runtime}, runtimeKey, "flow-1")
	if !found || isLegacy || got.ID != runtime.ID {
		t.Fatalf("pickZeroFlowUsage() = %+v found=%v legacy=%v, want runtime cursor", got, found, isLegacy)
	}
}

func TestPickZeroFlowUsageFallsBackToLegacyCursor(t *testing.T) {
	runtimeKey := zeroFlowUsageKey("core-a", "flow-1")
	legacy := model.FlowUsage{ID: 1, FlowID: "flow-1"}

	got, found, isLegacy := pickZeroFlowUsage([]model.FlowUsage{legacy}, runtimeKey, "flow-1")
	if !found || !isLegacy || got.ID != legacy.ID {
		t.Fatalf("pickZeroFlowUsage() = %+v found=%v legacy=%v, want legacy cursor", got, found, isLegacy)
	}
}

func TestPickZeroFlowUsageTreatsEmptyCandidatesAsNormalMiss(t *testing.T) {
	runtimeKey := zeroFlowUsageKey("core-a", "flow-1")
	got, found, isLegacy := pickZeroFlowUsage(nil, runtimeKey, "flow-1")
	if found || isLegacy || got.ID != 0 {
		t.Fatalf("empty candidates = %+v found=%v legacy=%v, want normal miss", got, found, isLegacy)
	}
}

func TestAggregateZeroFlowEventsKeepsLatestPerRuntimeFlow(t *testing.T) {
	base := time.Date(2026, 8, 10, 6, 0, 0, 0, time.UTC)
	events := []zeroevent.Envelope{
		{ID: "a-1", NodeID: 9, Type: "flow.updated", CoreInstanceID: "core-a", FlowID: "flow-1", Sequence: 10, OccurredAt: base},
		{ID: "a-2", NodeID: 9, Type: "flow.updated", CoreInstanceID: "core-a", FlowID: "flow-1", Sequence: 12, OccurredAt: base.Add(-time.Minute)},
		{ID: "b-1", NodeID: 9, Type: "flow.updated", CoreInstanceID: "core-b", FlowID: "flow-1", Sequence: 1, OccurredAt: base.Add(time.Second)},
		{ID: "stats", NodeID: 9, Type: "stats.sampled", CoreInstanceID: "core-a", Sequence: 13, OccurredAt: base.Add(2 * time.Second)},
	}

	got := aggregateZeroFlowEvents(events)
	if len(got) != 2 {
		t.Fatalf("flow projection count = %d, want 2 runtime generations", len(got))
	}
	ids := map[string]bool{}
	for _, event := range got {
		ids[event.ID] = true
	}
	if !ids["a-2"] || !ids["b-1"] || ids["a-1"] {
		t.Fatalf("unexpected coalesced flow events: %+v", got)
	}
}

func TestZeroRuntimeFlowEventRejectsOldSequence(t *testing.T) {
	usage := model.FlowUsage{
		FlowID:   zeroFlowUsageKey("core-a", "flow-1"),
		Revision: 20,
	}
	event := zeroEventEnvelope{CoreInstanceID: "core-a", Sequence: 19}
	if !zeroRuntimeFlowEventIsStale(usage, event, zeroFlowProjection{Revision: 30}) {
		t.Fatal("older sequence must not advance a runtime-scoped flow cursor")
	}
	event.Sequence = 21
	if zeroRuntimeFlowEventIsStale(usage, event, zeroFlowProjection{Revision: 1}) {
		t.Fatal("newer sequence must advance a runtime-scoped flow cursor")
	}
}

func TestZeroRuntimeFlowCountersDetectRegression(t *testing.T) {
	usage := model.FlowUsage{RawBytes: 300, UploadBytes: 100, DownloadBytes: 200}
	if !zeroRuntimeFlowCountersRegress(usage, 250, zeroFlowProjection{BytesUp: 100, BytesDown: 150}) {
		t.Fatal("regressed cumulative totals must be rejected within one runtime flow")
	}
	if zeroRuntimeFlowCountersRegress(usage, 400, zeroFlowProjection{BytesUp: 150, BytesDown: 250}) {
		t.Fatal("monotonic cumulative totals must be accepted")
	}
}
