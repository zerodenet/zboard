package network

import (
	"testing"
	"time"
)

func TestEventComparisonsIgnoreSourceMetadata(t *testing.T) {
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name           string
		left, right    EventPosition
		newer, advance bool
	}{
		{
			name:  "same runtime sequence precedes time",
			left:  EventPosition{ID: "a", CoreInstanceID: "runtime-a", Sequence: 12, OccurredAt: base},
			right: EventPosition{ID: "z", CoreInstanceID: "runtime-a", Sequence: 11, OccurredAt: base.Add(time.Hour)},
			newer: true, advance: true,
		},
		{
			name:  "older same runtime sequence remains stale",
			left:  EventPosition{CoreInstanceID: "runtime-a", Sequence: 11, OccurredAt: base.Add(time.Hour)},
			right: EventPosition{CoreInstanceID: "runtime-a", Sequence: 12, OccurredAt: base},
		},
		{
			name:  "different runtimes use occurred time",
			left:  EventPosition{CoreInstanceID: "runtime-b", Sequence: 1, OccurredAt: base.Add(time.Second)},
			right: EventPosition{CoreInstanceID: "runtime-a", Sequence: 100, OccurredAt: base},
			newer: true, advance: true,
		},
		{
			name:  "equal time uses config revision",
			left:  EventPosition{ConfigRevision: 3, Sequence: 1, OccurredAt: base},
			right: EventPosition{ConfigRevision: 2, Sequence: 100, OccurredAt: base},
			newer: true, advance: true,
		},
		{
			name:  "legacy equal time and revision use sequence",
			left:  EventPosition{Sequence: 2, OccurredAt: base},
			right: EventPosition{Sequence: 1, OccurredAt: base},
			newer: true, advance: true,
		},
		{
			name:  "event id only breaks batch ties",
			left:  EventPosition{ID: "z", OccurredAt: base},
			right: EventPosition{ID: "a", OccurredAt: base},
			newer: true,
		},
		{
			name:  "equal positions stay equal",
			left:  EventPosition{ID: "same", OccurredAt: base},
			right: EventPosition{ID: "same", OccurredAt: base},
		},
	}
	metadata := []struct {
		leftSource, rightSource     string
		leftReceived, rightReceived time.Time
	}{
		{"", "", time.Time{}, time.Time{}},
		{"sender-z", "sender-a", base.Add(time.Hour), base.Add(-time.Hour)},
		{"sender-a", "sender-z", base.Add(-time.Hour), base.Add(time.Hour)},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			for _, source := range metadata {
				left, right := item.left, item.right
				left.SourceID, right.SourceID = source.leftSource, source.rightSource
				left.ReceivedAt, right.ReceivedAt = source.leftReceived, source.rightReceived
				if got := EventNewer(left, right); got != item.newer {
					t.Fatalf("source metadata changed batch comparison: got %t want %t", got, item.newer)
				}
				if got := EventNewerThanPosition(left, right); got != item.advance {
					t.Fatalf("source metadata changed cursor comparison: got %t want %t", got, item.advance)
				}
			}
		})
	}
}
