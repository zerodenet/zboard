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
func readSubscriptionSettlementIdentity(db *gorm.DB, id, expectedBuyer uint) (*model.Order, error) {
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
	if err != nil || (full == nil && (identity.OrderType != "renewal" || identity.TargetSubscriptionID == nil || identity.Status == "paid")) {
		return nil, err
	}
	return &identity, nil
}
func validateSubscriptionSettlementIdentity(current model.Order, prepared *model.Order) error {
	// A paid renewal replay has no old-term mutation. Keep its original
	// idempotent path independent of the number of historical target orders.
	// If its state changed since the outside read, require the full identity.
	if prepared == nil && current.OrderType == "renewal" && current.Status == "paid" {
		return nil
	}
	currentRenewal := current.OrderType == "renewal" && current.TargetSubscriptionID != nil
	preparedRenewal := prepared != nil && prepared.OrderType == "renewal" && prepared.TargetSubscriptionID != nil
	if currentRenewal || preparedRenewal {
		if !currentRenewal || !preparedRenewal || current.ID != prepared.ID || current.UserID != prepared.UserID || *current.TargetSubscriptionID != *prepared.TargetSubscriptionID || current.PurchaseSnapshot != prepared.PurchaseSnapshot {
			return commerce.ErrSubscriptionQuoteChanged
		}
		return nil
	}
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

// Full changes consume older paid sources, and recovery renewals finalize
// historical orders. Both acquire all related orders in the same order as
// unsubscribe and automatic renewal, before user/subscription locks. Verify
// the pre-read immutable target against the current locked order.
func lockSubscriptionSettlementScope(tx *gorm.DB, identity *model.Order, actor uint, admin bool) error {
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
			if err := validateSubscriptionSettlementIdentity(locked, identity); err != nil {
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
