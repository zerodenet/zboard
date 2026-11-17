package handler

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

func TestZeroNodeObservationPreservesIndependentSourceMaterial(t *testing.T) {
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name          string
		latest, stats zeroevent.Envelope
	}{
		{
			name:   "sequence wins despite reversed reception and occurrence",
			latest: zeroevent.Envelope{ID: "flow-new", NodeID: 7, SourceID: " node-007-flow ", Type: "flow.updated", CoreInstanceID: "runtime-a", Sequence: 12, OccurredAt: base, ReceivedAt: base.Add(time.Second)},
			stats:  zeroevent.Envelope{ID: "stats-earlier", NodeID: 7, SourceID: "node-7-stats", Type: "stats.sampled", CoreInstanceID: "runtime-a", Sequence: 11, OccurredAt: base.Add(time.Hour), ReceivedAt: base.Add(2 * time.Hour)},
		},
		{
			name:   "different runtime sources stay independent",
			latest: zeroevent.Envelope{ID: "flow-runtime-b", NodeID: 7, SourceID: "node-7-new", Type: "flow.updated", CoreInstanceID: "runtime-b", Sequence: 1, OccurredAt: base.Add(time.Second), ReceivedAt: base},
			stats:  zeroevent.Envelope{ID: "stats-runtime-a", NodeID: 7, SourceID: "node-7-old", Type: "stats.sampled", CoreInstanceID: "runtime-a", Sequence: 100, OccurredAt: base, ReceivedAt: base.Add(time.Hour)},
		},
		{
			name:   "legacy missing source and receipt remain unknown",
			latest: zeroevent.Envelope{ID: "legacy-flow", NodeID: 7, Type: "flow.updated", Sequence: 12, OccurredAt: base.Add(time.Second)},
			stats:  zeroevent.Envelope{ID: "legacy-stats", NodeID: 7, Type: "stats.sampled", Sequence: 11, OccurredAt: base},
		},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			item.stats.Payload = json.RawMessage(`{"active_sessions":3,"bytes_up":100,"bytes_down":200}`)
			// A later iteration/receipt must not override the original comparator.
			projection := aggregateZeroNodeEvents([]zeroevent.Envelope{item.latest, item.stats})[7]
			observation := zeroNodeObservation(projection)
			if observation.NodeID != 7 || observation.Latest.ID != item.latest.ID || observation.StatsEvent == nil || observation.StatsEvent.ID != item.stats.ID {
				t.Fatal("aggregation changed the selected independent source events", observation)
			}
			if observation.Latest.SourceID != item.latest.SourceID || observation.Latest.ReceivedAt != item.latest.ReceivedAt || observation.Latest.OccurredAt != item.latest.OccurredAt {
				t.Fatal("latest source material was normalized, replaced or lost", observation.Latest)
			}
			if observation.StatsEvent.SourceID != item.stats.SourceID || observation.StatsEvent.ReceivedAt != item.stats.ReceivedAt || observation.StatsEvent.OccurredAt != item.stats.OccurredAt {
				t.Fatal("stats borrowed latest material or lost its own original source", observation.StatsEvent)
			}
			if observation.Stats.ActiveSessions != 3 || observation.Stats.BytesUp != 100 || observation.Stats.BytesDown != 200 {
				t.Fatal("source metadata changed selected counters", observation.Stats)
			}
			if repeated := zeroNodeObservation(projection); !reflect.DeepEqual(repeated, observation) {
				t.Fatal("repeated mapping regenerated source material")
			}
		})
	}
}

func TestZeroNodeObservationDoesNotInventStatsSource(t *testing.T) {
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	event := zeroevent.Envelope{ID: "flow-only", NodeID: 7, SourceID: "node-7", Type: "flow.updated", CoreInstanceID: "runtime-a", Sequence: 12, OccurredAt: base, ReceivedAt: base.Add(time.Second)}
	projection := aggregateZeroNodeEvents([]zeroevent.Envelope{event})[7]
	observation := zeroNodeObservation(projection)
	if observation.StatsEvent != nil || observation.Stats.ActiveSessions != 0 || observation.Stats.BytesUp != 0 || observation.Stats.BytesDown != 0 {
		t.Fatal("flow-only latest material was promoted into a stats sample", observation)
	}
	if observation.Latest.SourceID != event.SourceID || observation.Latest.ReceivedAt != event.ReceivedAt {
		t.Fatal("flow-only latest source material was lost", observation.Latest)
	}
}

func TestNetworkEventPositionKeepsLegacyZeroMaterialStable(t *testing.T) {
	event := zeroevent.Envelope{ID: "legacy-unknown", NodeID: 7, Type: "stats.sampled"}
	position := networkEventPosition(event)
	if position.SourceID != "" || !position.ReceivedAt.IsZero() || !position.OccurredAt.IsZero() {
		t.Fatal("legacy unknown time/source was replaced by mapping-time metadata", position)
	}
	if repeated := networkEventPosition(event); repeated != position {
		t.Fatal("legacy mapping was not stable across replay")
	}
}

func TestAggregateZeroNodeEventsCoalescesNodeWrites(t *testing.T) {
	base := time.Date(2026, 8, 10, 5, 0, 0, 0, time.UTC)
	events := []zeroevent.Envelope{
		{ID: "flow-1", NodeID: 7, Type: "flow.updated", CoreInstanceID: "core-a", Sequence: 10, OccurredAt: base},
		{ID: "stats-1", NodeID: 7, Type: "stats.sampled", CoreInstanceID: "core-a", Sequence: 11, OccurredAt: base.Add(time.Second), Payload: json.RawMessage(`{"active_sessions":3,"bytes_up":100,"bytes_down":200}`)},
		{ID: "flow-2", NodeID: 7, Type: "flow.updated", CoreInstanceID: "core-a", Sequence: 12, OccurredAt: base.Add(2 * time.Second)},
		{ID: "stats-2", NodeID: 7, Type: "stats.sampled", CoreInstanceID: "core-a", Sequence: 13, OccurredAt: base.Add(3 * time.Second), Payload: json.RawMessage(`{"active_sessions":4,"bytes_up":150,"bytes_down":260}`)},
	}

	projections := aggregateZeroNodeEvents(events)
	if len(projections) != 1 {
		t.Fatalf("expected one node projection, got %d", len(projections))
	}
	projection := projections[7]
	if projection.Latest.ID != "stats-2" {
		t.Fatalf("unexpected latest event: %+v", projection.Latest)
	}
	if projection.StatsEvent == nil || projection.StatsEvent.ID != "stats-2" {
		t.Fatalf("unexpected latest stats event: %+v", projection.StatsEvent)
	}
	if projection.Stats.ActiveSessions != 4 || projection.Stats.BytesUp != 150 || projection.Stats.BytesDown != 260 {
		t.Fatalf("unexpected stats projection: %+v", projection.Stats)
	}
}

func TestZeroEventNewerUsesSequenceWithinCoreInstance(t *testing.T) {
	newerTime := time.Date(2026, 8, 10, 5, 0, 10, 0, time.UTC)
	olderSequence := zeroevent.Envelope{ID: "old", CoreInstanceID: "core-a", Sequence: 8, OccurredAt: newerTime}
	newerSequence := zeroevent.Envelope{ID: "new", CoreInstanceID: "core-a", Sequence: 9, OccurredAt: newerTime.Add(-time.Minute)}
	if !zeroEventNewer(newerSequence, olderSequence) {
		t.Fatal("same-instance sequence must win over wall-clock order")
	}
	if zeroEventNewer(olderSequence, newerSequence) {
		t.Fatal("older same-instance sequence must not replace newer state")
	}
}

func TestZeroEnvelopeNewerThanCursorRejectsLateOldSequence(t *testing.T) {
	cursor := zeroEventNodeCursor{
		CoreInstanceID: "core-a",
		Sequence:       20,
		OccurredAt:     time.Date(2026, 8, 10, 5, 0, 0, 0, time.UTC),
	}
	lateOld := zeroevent.Envelope{
		CoreInstanceID: "core-a",
		Sequence:       19,
		OccurredAt:     cursor.OccurredAt.Add(time.Hour),
	}
	if zeroEnvelopeNewerThanCursor(lateOld, cursor) {
		t.Fatal("late older sequence must not regress a node projection")
	}
}

func TestZeroEnvelopeNewerThanCursorUsesTimeAcrossCoreInstances(t *testing.T) {
	cursor := zeroEventNodeCursor{
		CoreInstanceID: "core-a",
		Sequence:       500,
		OccurredAt:     time.Date(2026, 8, 10, 5, 0, 0, 0, time.UTC),
	}
	newGeneration := zeroevent.Envelope{
		CoreInstanceID: "core-b",
		Sequence:       1,
		OccurredAt:     cursor.OccurredAt.Add(time.Second),
	}
	if !zeroEnvelopeNewerThanCursor(newGeneration, cursor) {
		t.Fatal("new core instance must be orderable without comparing reset sequences")
	}
}

func TestZeroBufferedFlowIDReadsNestedRecord(t *testing.T) {
	payload := json.RawMessage(`{"record":{"flow_id":"flow-123"}}`)
	if got := zeroBufferedFlowID(payload); got != "flow-123" {
		t.Fatalf("unexpected flow id %q", got)
	}
}

func TestZeroEventConsumerPressureCadence(t *testing.T) {
	base := 5 * time.Second
	cases := []struct {
		name     string
		status   zeroevent.Status
		interval time.Duration
		burst    int
	}{
		{name: "normal", status: zeroevent.Status{}, interval: 5 * time.Second, burst: 8},
		{name: "warning", status: zeroevent.Status{Warning: true}, interval: 2500 * time.Millisecond, burst: 12},
		{name: "compact", status: zeroevent.Status{Warning: true, Compact: true}, interval: 1250 * time.Millisecond, burst: 16},
		{name: "emergency", status: zeroevent.Status{Warning: true, Compact: true, Emergency: true}, interval: 500 * time.Millisecond, burst: 32},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if got := zeroEventConsumerInterval(base, item.status); got != item.interval {
				t.Fatalf("interval = %s, want %s", got, item.interval)
			}
			if got := zeroEventConsumerBurst(item.status); got != item.burst {
				t.Fatalf("burst = %d, want %d", got, item.burst)
			}
		})
	}
}

func TestZeroEventConsumerIntervalHasFloor(t *testing.T) {
	status := zeroevent.Status{Emergency: true}
	if got := zeroEventConsumerInterval(200*time.Millisecond, status); got != zeroEventConsumerMinimumInterval {
		t.Fatalf("interval = %s, want floor %s", got, zeroEventConsumerMinimumInterval)
	}
}
