package networkstore

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"gorm.io/gorm"
)

func observationPosition(c ObservationCursor) network.EventPosition {
	return network.EventPosition{CoreInstanceID: c.CoreInstanceID, Sequence: c.Sequence, ConfigRevision: c.ConfigRevision, OccurredAt: c.OccurredAt}
}

// ProjectObservation hands off bounded current-state data. Unattributed legacy
// sources are ignored; projection is exclusively the independent worker.
func ProjectObservation(tx *gorm.DB, p network.NodeObservation) error {
	err := observationstore.EnqueueLatest(tx, []network.ObservationSource{{Observation: p, Kind: "legacy_projection"}})
	return err
}
