package commercestore

import (
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sort"
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
	// A closed/canceled historical attempt can fall outside the current
	// entitlement source set. It must still be locked before owner so verified
	// late cash can reach its existing credit-only path. Preserve the source
	// set for revalidation; union only the actual lock set in global PK order.
	locking := identity.Scope
	locking.IDs = append([]uint(nil), identity.Scope.IDs...)
	i := sort.Search(len(locking.IDs), func(i int) bool { return locking.IDs[i] >= identity.ID })
	if i == len(locking.IDs) || locking.IDs[i] != identity.ID {
		if len(locking.IDs) >= orderIdentityByteLimit/8 {
			return commerce.ErrSubscriptionEvidenceBudget
		}
		locking.IDs = append(locking.IDs, 0)
		copy(locking.IDs[i+1:], locking.IDs[i:])
		locking.IDs[i] = identity.ID
	}
	if err := lockSubscriptionOrderIDs(tx, locking); err != nil {
		return err
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
	err := verifySubscriptionOrderScope(tx, identity.Scope)
	if errors.Is(err, commerce.ErrSubscriptionQuoteChanged) {
		return errSubscriptionSettlementScopeChanged
	}
	return err
}

// This identity denotes only an unlocked preparation that became stale before
// entering financial logic. Immutable checkout changes and quote/funding errors
// from apply are intentionally not converted to this retryable condition.
var errSubscriptionSettlementScopeChanged = fmt.Errorf("subscription settlement source scope changed before financial work: %w", commerce.ErrSubscriptionQuoteChanged)

func withSubscriptionSettlementScope(db *gorm.DB, id, expectedBuyer, actor uint, admin bool, apply func(*gorm.DB, *subscriptionSettlementIdentity) error) error {
	for attempt := 0; attempt < 2; attempt++ {
		identity, err := readSubscriptionSettlementIdentity(db, id, expectedBuyer)
		if err != nil {
			return err
		}
		err = db.Transaction(func(tx *gorm.DB) error {
			if err := lockSubscriptionSettlementScope(tx, identity, actor, admin); err != nil {
				return err
			}
			return apply(tx, identity)
		})
		if !errors.Is(err, errSubscriptionSettlementScopeChanged) {
			return err
		}
		// The callback never ran: no event, receipt, wallet or entitlement write
		// occurred. Transaction has rolled back its locks before rediscovery.
	}
	return errSubscriptionSettlementScopeChanged
}
