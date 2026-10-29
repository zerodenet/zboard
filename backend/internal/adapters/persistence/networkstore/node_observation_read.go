package networkstore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// One SELECT ties values and provenance to the same read view. A second query
// after loading node counters could otherwise label old values with new evidence.
func withObservationEvidence(db *gorm.DB) *gorm.DB {
	asset := "nodes.asset_tag <> '' AND observation_state.asset_tag = nodes.asset_tag"
	if db.Dialector.Name() == "mysql" {
		asset = "nodes.asset_tag <> '' AND BINARY observation_state.asset_tag = BINARY nodes.asset_tag"
	}
	return db.Joins("LEFT JOIN node_observation_states observation_state ON observation_state.node_id = nodes.id AND " + asset).Select("nodes.*, COALESCE(observation_state.current_known, 0) AS observation_known, COALESCE(observation_state.latest_known, 0) AS observation_latest_source_known, COALESCE(observation_state.stats_known, 0) AS observation_stats_source_known, COALESCE(observation_state.current_source, 'unknown') AS observation_source, observation_state.latest_reference AS observation_latest_reference, observation_state.stats_reference AS observation_stats_reference, observation_state.counter_written_at AS observation_counter_received_at, observation_state.latest_occurred_at AS observation_latest_occurred_at, observation_state.latest_received_at AS observation_latest_received_at, observation_state.stats_occurred_at AS observation_stats_occurred_at, observation_state.stats_received_at AS observation_stats_received_at")
}
func nodeCounterEvidence(n model.Node) network.CounterEvidence {
	source := n.ObservationSource
	if source == "" {
		source = "unknown"
	}
	return network.CounterEvidence{Known: n.ObservationKnown, LatestSourceKnown: n.ObservationLatestSourceKnown, StatsSourceKnown: n.ObservationStatsSourceKnown, Source: source, LatestReference: n.ObservationLatestReference, StatsReference: n.ObservationStatsReference, CounterReceivedAt: n.ObservationCounterReceivedAt, LatestOccurredAt: n.ObservationLatestOccurredAt, LatestReceivedAt: n.ObservationLatestReceivedAt, StatsOccurredAt: n.ObservationStatsOccurredAt, StatsReceivedAt: n.ObservationStatsReceivedAt}
}
