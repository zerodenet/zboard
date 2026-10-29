package handler

import (
	networkcap "github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

// Preserve the legacy node fields while exposing the same provenance contract
// as the paged list/detail. Private ORM evidence fields remain JSON-excluded.
type nodeLegacyListItem struct {
	model.Node
	Observation networkcap.CounterEvidence `json:"observation"`
}

func nodeObservationEvidence(node model.Node) networkcap.CounterEvidence {
	source := node.ObservationSource
	if source == "" {
		source = "unknown"
	}
	return networkcap.CounterEvidence{
		Known: node.ObservationKnown, Source: source,
		LatestSourceKnown: node.ObservationLatestSourceKnown, StatsSourceKnown: node.ObservationStatsSourceKnown,
		LatestReference: node.ObservationLatestReference, StatsReference: node.ObservationStatsReference,
		CounterReceivedAt: node.ObservationCounterReceivedAt,
		LatestOccurredAt:  node.ObservationLatestOccurredAt, LatestReceivedAt: node.ObservationLatestReceivedAt,
		StatsOccurredAt: node.ObservationStatsOccurredAt, StatsReceivedAt: node.ObservationStatsReceivedAt,
	}
}
