package entitlementstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type ClientAccess struct{ DB *gorm.DB }

func (s ClientAccess) Resolve(ctx context.Context, hash string) (entitlements.ClientGrant, error) {
	var out entitlements.ClientGrant
	var inactive bool
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var identity model.SubscriptionToken
		if err := tx.Select("id", "user_id", "subscription_id").Where("token_hash = ? AND revoked_at IS NULL", hash).First(&identity).Error; err != nil {
			return clientAccessError(err)
		}
		if identity.SubscriptionID == nil || *identity.SubscriptionID == 0 {
			return entitlements.ErrAccessPermission
		}
		// Follow account rotation's user-before-token order. Re-read the presented
		// token after waiting so a concurrent rotation cannot authorize stale access.
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status").First(&user, identity.UserID).Error; err != nil {
			return clientAccessError(err)
		}
		if user.Status != "active" {
			return entitlements.ErrAccessPermission
		}
		var access model.SubscriptionToken
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "user_id", "subscription_id").Where("id = ? AND user_id = ? AND token_hash = ? AND revoked_at IS NULL", identity.ID, user.ID, hash).First(&access).Error; err != nil {
			return clientAccessError(err)
		}
		if access.SubscriptionID == nil || *access.SubscriptionID == 0 {
			return entitlements.ErrAccessPermission
		}
		now := time.Now().UTC()
		if err := ExpireInTransaction(tx, user.ID, now); err != nil {
			return err
		}
		var sub model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", *access.SubscriptionID, user.ID).First(&sub).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				inactive = true
				return nil
			}
			return err
		}
		if !entitlements.AccessAvailable(entitlements.Subscription(sub), now) {
			inactive = true
			return nil
		}
		out = entitlements.ClientGrant{Subscription: entitlements.Subscription(sub), AccessID: access.ID, ObservedAt: now}
		return nil
	})
	if err != nil {
		return entitlements.ClientGrant{}, err
	}
	if inactive {
		return entitlements.ClientGrant{}, entitlements.ErrAccessInactive
	}
	return out, nil
}
func clientAccessError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return entitlements.ErrAccessPermission
	}
	return err
}
func (s ClientAccess) MarkUsed(ctx context.Context, hash string, id uint, at time.Time) error {
	// A render started before rotation/revocation must not mark its replacement used.
	return s.DB.WithContext(ctx).Model(&model.SubscriptionToken{}).Where("id = ? AND token_hash = ? AND revoked_at IS NULL", id, hash).Update("last_used_at", at.UTC()).Error
}
