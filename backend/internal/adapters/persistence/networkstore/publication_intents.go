package networkstore

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/publicationstore"
	"gorm.io/gorm"
)

// Business intents and schema recovery share the same generation/lease rules.
func EnqueuePublication(tx *gorm.DB, nodeID, endpointID, actor uint) error {
	return publicationstore.Enqueue(tx, nodeID, endpointID, actor)
}
