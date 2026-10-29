package networkstore

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"gorm.io/gorm"
)

func observationPosition(c ObservationCursor) network.EventPosition {
	return network.EventPosition{CoreInstanceID: c.CoreInstanceID, Sequence: c.Sequence, ConfigRevision: c.ConfigRevision, OccurredAt: c.OccurredAt}
}

// ProjectObservation now hands off immutable material. Missing legacy source
// attribution stays held; projection is exclusively the independent worker.
func ProjectObservation(tx *gorm.DB, p network.NodeObservation) error {
	_, err := observationstore.Enqueue(tx, network.ObservationSource{Observation: p, Kind: "legacy_projection"})
	return err
}
