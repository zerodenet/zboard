package commercestore

import (
	"fmt"
	"sort"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const closureOrderBudget = 4096

func closureOrders(db *gorm.DB, subscriptions []uint) *gorm.DB {
	held := db.Model(&model.OrderFunding{}).Select("1").Where("order_fundings.order_id = orders.id AND order_fundings.state = ?", "reserved")
	return db.Model(&model.Order{}).Where("subscription_id IN ? OR target_subscription_id IN ?", subscriptions, subscriptions).
		Where("subscription_ended_at IS NULL OR status = ? OR (status IN ? AND EXISTS (?))", "pending", []string{"canceled", "failed"}, held).
		Order("id ASC").Limit(closureOrderBudget + 1)
}

// Prepare immutable IDs before the authority transaction. A consistent read
// before waiting on owner locks would leave MySQL funding release on an old RR
// snapshot. Inside, lock exact primary keys, then owners, and recheck membership
// against the lifecycle owner's current locked subscriptions.
func PrepareLifecycleClosureScope(db *gorm.DB, candidates []model.Subscription) (entitlementstore.CredentialExpiryScope, error) {
	ids := make([]uint, 0, len(candidates))
	owners := []uint{}
	seen := map[uint]bool{}
	for _, sub := range candidates {
		ids = append(ids, sub.ID)
		if !seen[sub.UserID] {
			owners = append(owners, sub.UserID)
			seen[sub.UserID] = true
		}
	}
	sort.Slice(owners, func(i, j int) bool { return owners[i] < owners[j] })
	var prepared []uint
	if err := closureOrders(db, ids).Pluck("id", &prepared).Error; err != nil {
		return entitlementstore.CredentialExpiryScope{}, err
	}
	if len(prepared) > closureOrderBudget {
		return entitlementstore.CredentialExpiryScope{}, entitlements.ErrCredentialExpiryScopeBudget
	}
	preparedSet := map[uint]bool{}
	for _, id := range prepared {
		preparedSet[id] = true
	}
	lock := func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "sqlite" && len(ids) > 0 {
			if err := tx.Exec("UPDATE subscriptions SET status = status WHERE id = ?", ids[0]).Error; err != nil {
				return err
			}
		}
		if len(prepared) > 0 {
			var locked []uint
			if err := tx.Model(&model.Order{}).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", prepared).Order("id ASC").Pluck("id", &locked).Error; err != nil {
				return err
			}
			if len(locked) != len(prepared) {
				return fmt.Errorf("expiry order scope changed before locking; retry")
			}
		}
		var lockedOwners []uint
		if err := tx.Model(&model.User{}).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", owners).Order("id ASC").Pluck("id", &lockedOwners).Error; err != nil {
			return err
		}
		if len(lockedOwners) != len(owners) {
			return fmt.Errorf("expiry owner missing; restore before closing financial state")
		}
		return nil
	}
	validate := func(tx *gorm.DB, due []model.Subscription) error {
		if len(due) == 0 {
			return nil
		}
		dueIDs := make([]uint, 0, len(due))
		for _, sub := range due {
			dueIDs = append(dueIDs, sub.ID)
		}
		var current []uint
		// Owners are already locked and no consistent read occurred before
		// those locks. Inspect current committed membership without taking
		// secondary-index ranges against a payment waiting on our primary
		// order locks. Every required ID must still be in the locked set.
		if err := closureOrders(tx, dueIDs).Pluck("id", &current).Error; err != nil {
			return err
		}
		if len(current) > closureOrderBudget {
			return entitlements.ErrCredentialExpiryScopeBudget
		}
		for _, id := range current {
			if !preparedSet[id] {
				return fmt.Errorf("expiry order membership changed before owner lock; retry")
			}
		}
		return nil
	}
	return entitlementstore.CredentialExpiryScope{Lock: lock, Validate: validate}, nil
}
