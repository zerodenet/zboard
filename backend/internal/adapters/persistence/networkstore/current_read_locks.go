package networkstore

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func currentResourceReadLock(tx *gorm.DB) clause.Locking {
	return credentialstore.CurrentResourceReadLock(tx)
}
func currentResourceReadError(err error) error {
	return credentialstore.CurrentResourceReadError(err)
}
func lockCurrentResourceNodes(tx *gorm.DB, ids []uint) error {
	return credentialstore.LockCurrentResourceNodes(tx, ids)
}
