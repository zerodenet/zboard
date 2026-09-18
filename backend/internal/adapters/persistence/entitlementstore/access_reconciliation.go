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

// ReconcileAccessTokens is invoked by application startup, not a public API.
// Keyset batches bound the scan; each provision rechecks authoritative rows.
func ReconcileAccessTokens(ctx context.Context, db *gorm.DB, cipher entitlements.AccessCipher) error {
	var after uint
	for {
		var candidates []model.Subscription
		if err := db.WithContext(ctx).Model(&model.Subscription{}).Select("subscriptions.id, subscriptions.user_id").Joins("LEFT JOIN subscription_tokens ON subscription_tokens.subscription_id = subscriptions.id").Where("subscriptions.id > ? AND subscriptions.status = ? AND subscriptions.end_at > ? AND subscriptions.flow_used < subscriptions.flow_total", after, "active", time.Now().UTC()).Where("subscription_tokens.id IS NULL").Order("subscriptions.id asc").Limit(200).Find(&candidates).Error; err != nil {
			return err
		}
		if len(candidates) == 0 {
			return nil
		}
		for _, candidate := range candidates {
			if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				var user model.User
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status").First(&user, candidate.UserID).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return nil
					}
					return err
				}
				if user.Status != "active" {
					return nil
				}
				var sub model.Subscription
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", candidate.ID, user.ID).First(&sub).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return nil
					}
					return err
				}
				if !entitlements.AccessAvailable(entitlements.Subscription(sub), time.Now().UTC()) {
					return nil
				}
				_, _, err := EnsureAccessToken(tx, sub, cipher)
				return err
			}); err != nil {
				return err
			}
			after = candidate.ID
		}
	}
}
