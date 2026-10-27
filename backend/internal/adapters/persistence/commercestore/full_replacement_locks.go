package commercestore

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func relatedSubscriptionOrders(tx *gorm.DB, user, subscription uint, start time.Time) *gorm.DB {
	return terminationOrders(tx, user, subscription, start)
}

type subscriptionSettlementIdentity struct {
	model.Order
	Scope subscriptionOrderScope
}

// Read immutable checkout identity before beginning the authority transaction.
// A non-locking read inside MySQL's transaction would establish a stale repeatable
// read snapshot before waiting on the order, breaking ordinary payment replay.
func readSubscriptionSettlementIdentity(db *gorm.DB, id, expectedBuyer uint) (*subscriptionSettlementIdentity, error) {
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
	scope, err := prepareSubscriptionOrderScope(db, identity.UserID, *identity.TargetSubscriptionID)
	if err != nil {
		return nil, err
	}
	return &subscriptionSettlementIdentity{Order: identity, Scope: scope}, nil
}
func validateSubscriptionSettlementIdentity(current model.Order, prepared *subscriptionSettlementIdentity) error {
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
func lockSubscriptionSettlementScope(tx *gorm.DB, identity *subscriptionSettlementIdentity, actor uint, admin bool) error {
	if identity == nil {
		return nil
	}
	if err := lockSubscriptionOrderIDs(tx, identity.Scope); err != nil {
		return err
	}
	found := false
	for _, id := range identity.Scope.IDs {
		if id == identity.ID {
			found = true
			break
		}
	}
	if !found {
		return commerce.ErrSubscriptionQuoteChanged
	}
	var current model.Order
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, identity.ID).Error; err != nil {
		return err
	}
	if err := validateSubscriptionSettlementIdentity(current, identity); err != nil {
		return err
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
	return verifySubscriptionOrderScope(tx, identity.Scope)
}
