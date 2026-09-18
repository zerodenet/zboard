package handler

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"time"

	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"gorm.io/gorm"
)

const fairUseCoverageFreshness = 30 * time.Second

type fairUseNodeCoverage = meteringstore.CoverageRecord
type fairUseCoverageNode = metering.CoverageNode
type fairUseCoverageSummary = metering.CoverageSummary

func coverageInput(node uint, event zeroEventEnvelope, receivedAt time.Time) metering.NodeCoverageEvent {
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	return metering.NodeCoverageEvent{NodeID: node, ReceivedAt: receivedAt, Event: metering.CoverageEvent{CoreInstanceID: event.CoreInstanceID, Sequence: event.Sequence, EventID: event.EventID, EventType: event.EventType, OccurredAt: zeroEventTime(event, receivedAt)}}
}
func (h *handlers) observeFairUseEventCoverage(node uint, event zeroEventEnvelope, receivedAt time.Time) error {
	return h.services.ObserveFairUseCoverage(context.Background(), coverageInput(node, event, receivedAt))
}
func advanceFairUseCoverage(current fairUseNodeCoverage, exists bool, node uint, event zeroEventEnvelope, receivedAt time.Time) (fairUseNodeCoverage, bool) {
	input := coverageInput(node, event, receivedAt)
	next, changed := metering.AdvanceCoverage(metering.CoverageFact(current), exists, node, input.Event, input.ReceivedAt)
	return fairUseNodeCoverage(next), changed
}
func (h *handlers) projectFairUseCoverageBatch(tx *gorm.DB, events []zeroevent.Envelope) error {
	batch := make([]metering.NodeCoverageEvent, 0, len(events))
	now := time.Now().UTC()
	for _, event := range events {
		receivedAt := event.ReceivedAt
		if receivedAt.IsZero() {
			receivedAt = now
		}
		batch = append(batch, coverageInput(uint(event.NodeID), zeroBufferedEnvelopeAsEvent(event), receivedAt))
	}
	return application.ProjectFairUseCoverage(tx, batch)
}

func classifyFairUseCoverage(row fairUseNodeCoverage, exists bool, cutoff, freshCutoff time.Time) (string, string) {
	return metering.ClassifyCoverage(metering.CoverageFact(row), exists, cutoff, freshCutoff)
}
