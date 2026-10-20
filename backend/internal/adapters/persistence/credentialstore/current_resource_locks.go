package credentialstore

import (
	"errors"
	"sort"

	"github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Current topology reads must not introduce a reverse wait behind credential
// locks retained by a prior grant, revocation or another event in the batch.
// SQLite already serializes these writes through its enclosing transaction.
func CurrentResourceReadLock(tx *gorm.DB) clause.Locking {
	lock := clause.Locking{Strength: "SHARE"}
	if tx.Dialector.Name() == "mysql" {
		lock.Options = "NOWAIT"
	}
	return lock
}
func CurrentResourceReadError(err error) error {
	var failure *mysql.MySQLError
	if errors.As(err, &failure) && failure.Number == 3572 {
		return network.ErrResourcesBusy
	}
	return err
}

// Foreign-key checks on a new credential or publication would otherwise wait
// on a node placement edit after credential/endpoint locks have been retained.
// Only the exact current parent identities are covered, never unrelated nodes.
func LockCurrentResourceNodes(tx *gorm.DB, ids []uint) error {
	seen := make(map[uint]bool, len(ids))
	parents := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id != 0 && !seen[id] {
			seen[id] = true
			parents = append(parents, id)
		}
	}
	ids = parents
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) == 0 {
		return nil
	}
	var nodes []model.Node
	return CurrentResourceReadError(tx.Clauses(CurrentResourceReadLock(tx)).Select("id").Where("id IN ?", ids).Order("id").Find(&nodes).Error)
}

// LockCurrentEndpointParents protects only the parents used by this accounting
// fact. A disabled endpoint can still own late historical traffic. Zero endpoint
// identities keep the existing legacy attribution behavior.
func LockCurrentEndpointParents(tx *gorm.DB, sourceNodeID, endpointID uint) error {
	if endpointID != 0 {
		var endpoint model.ProtocolEndpoint
		if err := tx.Clauses(CurrentResourceReadLock(tx)).Select("id").Where("id = ?", endpointID).Find(&endpoint).Error; err != nil {
			return CurrentResourceReadError(err)
		}
	}
	return LockCurrentResourceNodes(tx, []uint{sourceNodeID})
}
