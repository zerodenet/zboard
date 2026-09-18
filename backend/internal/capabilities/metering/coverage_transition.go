package metering

import (
	"strings"
	"time"
)

type CoverageEvent struct {
	CoreInstanceID     string
	Sequence           uint64
	EventID, EventType string
	OccurredAt         time.Time
}
type NodeCoverageEvent struct {
	NodeID     uint
	Event      CoverageEvent
	ReceivedAt time.Time
}

func AdvanceCoverage(current CoverageFact, exists bool, nodeID uint, event CoverageEvent, receivedAt time.Time) (CoverageFact, bool) {
	coreInstanceID := strings.TrimSpace(event.CoreInstanceID)
	if nodeID == 0 || coreInstanceID == "" || event.Sequence == 0 {
		return current, false
	}
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	receivedAt = receivedAt.UTC()
	occurredAt := event.OccurredAt.UTC()
	if occurredAt.IsZero() {
		occurredAt = receivedAt
	}
	eventID := strings.TrimSpace(event.EventID)
	if !exists {
		return CoverageFact{
			NodeID: nodeID, CoreInstanceID: coreInstanceID, LastSequence: event.Sequence,
			LastEventID: eventID, ContinuousSinceAt: receivedAt, LastReceivedAt: receivedAt,
			LastEventOccurredAt: occurredAt, UpdatedAt: receivedAt,
		}, true
	}

	if current.CoreInstanceID != coreInstanceID {
		if !current.LastEventOccurredAt.IsZero() && occurredAt.Before(current.LastEventOccurredAt) && event.EventType != "engine.started" {
			return current, false
		}
		current.CoreInstanceID = coreInstanceID
		current.LastSequence = event.Sequence
		current.LastEventID = eventID
		current.ContinuousSinceAt = receivedAt
		current.LastReceivedAt = receivedAt
		current.LastEventOccurredAt = occurredAt
		current.LastGapFromSequence = 0
		current.LastGapToSequence = 0
		current.LastGapAt = nil
		current.UpdatedAt = receivedAt
		return current, true
	}

	receiptInterrupted := !current.LastReceivedAt.IsZero() && receivedAt.Sub(current.LastReceivedAt) > (30*time.Second)
	if event.Sequence <= current.LastSequence {
		current.LastReceivedAt = receivedAt
		current.UpdatedAt = receivedAt
		if receiptInterrupted {
			current.ContinuousSinceAt = receivedAt
		}
		return current, true
	}

	previousSequence := current.LastSequence
	current.LastSequence = event.Sequence
	current.LastEventID = eventID
	current.LastReceivedAt = receivedAt
	current.LastEventOccurredAt = occurredAt
	current.UpdatedAt = receivedAt
	if receiptInterrupted {
		current.ContinuousSinceAt = receivedAt
	}
	if event.Sequence > previousSequence+1 {
		missing := event.Sequence - previousSequence - 1
		gapAt := receivedAt
		current.ContinuousSinceAt = receivedAt
		current.LastGapFromSequence = previousSequence + 1
		current.LastGapToSequence = event.Sequence - 1
		current.LastGapAt = &gapAt
		current.GapCount += missing
	}
	return current, true
}
