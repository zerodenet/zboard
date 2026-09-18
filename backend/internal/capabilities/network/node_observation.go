package network

import (
	"strings"
	"time"
)

type EventPosition struct {
	ID, CoreInstanceID       string
	Sequence, ConfigRevision uint64
	OccurredAt               time.Time
}
type NodeStats struct{ ActiveSessions, BytesUp, BytesDown uint64 }
type NodeObservation struct {
	NodeID     uint
	Latest     EventPosition
	StatsEvent *EventPosition
	Stats      NodeStats
}

func EventNewer(left, right EventPosition) bool {
	leftInstance := strings.TrimSpace(left.CoreInstanceID)
	rightInstance := strings.TrimSpace(right.CoreInstanceID)
	if leftInstance != "" && leftInstance == rightInstance && left.Sequence > 0 && right.Sequence > 0 {
		return left.Sequence > right.Sequence
	}
	if !left.OccurredAt.Equal(right.OccurredAt) {
		return left.OccurredAt.After(right.OccurredAt)
	}
	if left.ConfigRevision != right.ConfigRevision {
		return left.ConfigRevision > right.ConfigRevision
	}
	if left.Sequence != right.Sequence {
		return left.Sequence > right.Sequence
	}
	return left.ID > right.ID
}

func EventNewerThanPosition(event, cursor EventPosition) bool {
	instanceID := strings.TrimSpace(event.CoreInstanceID)
	cursorInstanceID := strings.TrimSpace(cursor.CoreInstanceID)
	if instanceID != "" && instanceID == cursorInstanceID && event.Sequence > 0 && cursor.Sequence > 0 {
		return event.Sequence > cursor.Sequence
	}
	occurredAt := event.OccurredAt.UTC()
	if !occurredAt.Equal(cursor.OccurredAt.UTC()) {
		return occurredAt.After(cursor.OccurredAt.UTC())
	}
	if event.ConfigRevision != cursor.ConfigRevision {
		return event.ConfigRevision > cursor.ConfigRevision
	}
	return event.Sequence > cursor.Sequence
}
