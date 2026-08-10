package handler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

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
