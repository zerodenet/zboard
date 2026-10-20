package commercestore

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func relatedSubscriptionOrders(tx *gorm.DB, user, subscription uint) *gorm.DB {
	return tx.Where("user_id = ? AND (subscription_id = ? OR target_subscription_id = ?)", user, subscription, subscription).Order("id ASC").Limit(1001)
}

// Read immutable checkout identity before beginning the authority transaction.
// A non-locking read inside MySQL's transaction would establish a stale repeatable
// read snapshot before waiting on the order, breaking ordinary payment replay.
func readFullReplacementIdentity(db *gorm.DB, id, expectedBuyer uint) (*model.Order, error) {
	var identity model.Order
	query := db.Where("id = ?", id)
	if expectedBuyer != 0 {
		query = query.Where("user_id = ?", expectedBuyer)
	}
	if err := query.First(&identity).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	full, err := fullReplacementSnapshot(identity)
	if err != nil || full == nil {
		return nil, err
	}
	return &identity, nil
}
func validateReplacementIdentity(current model.Order, prepared *model.Order) error {
	full, err := fullReplacementSnapshot(current)
	if err != nil {
		return err
	}
	if full == nil && prepared == nil {
		return nil
	}
	if full == nil || prepared == nil || current.ID != prepared.ID || current.UserID != prepared.UserID || current.OrderType != prepared.OrderType || current.TargetSubscriptionID == nil || prepared.TargetSubscriptionID == nil || *current.TargetSubscriptionID != *prepared.TargetSubscriptionID || current.ChangeSnapshot != prepared.ChangeSnapshot {
		return commerce.ErrSubscriptionQuoteChanged
	}
	return nil
}

// Full changes consume older paid sources. Acquire all related orders in the
// same order as unsubscribe, before user/subscription locks, then verify the
// pre-read immutable identity against the locked current order.
func lockFullReplacementScope(tx *gorm.DB, identity *model.Order, actor uint, admin bool) error {
	if identity == nil {
		return nil
	}
	var orders []model.Order
	if err := relatedSubscriptionOrders(tx, identity.UserID, *identity.TargetSubscriptionID).Clauses(clause.Locking{Strength: "UPDATE"}).Find(&orders).Error; err != nil {
		return err
	}
	if len(orders) > 1000 {
		return commerce.ErrSubscriptionValueUnavailable
	}
	found := false
	for _, locked := range orders {
		if locked.ID == identity.ID {
			found = true
			if err := validateReplacementIdentity(locked, identity); err != nil {
				return err
			}
			break
		}
	}
	if !found {
		return commerce.ErrSubscriptionQuoteChanged
	}
	if admin {
		if _, err := lockSettlementUsers(tx, identity.UserID, actor); err != nil {
			return err
		}
	} else {
		var owner model.User
		if actor != identity.UserID {
			return commerce.ErrOrderPermission
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status").First(&owner, actor).Error; err != nil {
			return resourceError(err)
		}
		if owner.Status != "active" {
			return commerce.ErrOrderPermission
		}
	}
	var current []uint
	if err := relatedSubscriptionOrders(tx, identity.UserID, *identity.TargetSubscriptionID).Model(&model.Order{}).Clauses(clause.Locking{Strength: "SHARE", Options: "NOWAIT"}).Pluck("id", &current).Error; err != nil {
		return err
	}
	if len(current) != len(orders) {
		return commerce.ErrSubscriptionQuoteChanged
	}
	for i, id := range current {
		if orders[i].ID != id {
			return commerce.ErrSubscriptionQuoteChanged
		}
	}
	return nil
}
