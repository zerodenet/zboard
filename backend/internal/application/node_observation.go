package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"gorm.io/gorm"
)

// ProjectNodeObservation participates in the same transaction as flow accounting.
func ProjectNodeObservation(tx *gorm.DB, in network.NodeObservation) error {
	return networkstore.ProjectObservation(tx, in)
}
