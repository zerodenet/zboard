package entitlementstore

import (
	"context"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const retirementBudgetRetryDelay = 30 * time.Minute

// A scheduling hold is not financial closure. Lock only owner -> subscription;
// no order/funding write or subsequent order lock is permitted in this path.
func (s CredentialExpiry) deferRetirement(ctx context.Context, candidate model.Subscription, now time.Time, reason string) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var owner model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&owner, candidate.UserID).Error; err != nil {
			return err
		}
		grace, err := RecoveryGracePeriod(tx)
		if err != nil {
			return err
		}
		var subs []model.Subscription
		if err := subscriptionLifecycleDue(tx.Clauses(clause.Locking{Strength: "UPDATE"}), now, grace).
			Where("id = ?", candidate.ID).Limit(1).Find(&subs).Error; err != nil {
			return err
		}
		if len(subs) == 0 {
			return nil
		}
		sub := subs[0]
		if sub.UserID != candidate.UserID || sub.EntitlementEpoch != candidate.EntitlementEpoch || !sub.StartAt.Equal(candidate.StartAt) || !sub.EndAt.Equal(candidate.EndAt) || !equalRetirementTime(sub.EndedAt, candidate.EndedAt) {
			return nil
		}
		if reason == "mutable_order_budget" {
			deadline := entitlements.RenewalDeadline(entitlements.Subscription(sub), now, grace)
			if deadline != nil && deadline.After(now) {
				return nil
			}
		}
		retry := now.UTC().Add(retirementBudgetRetryDelay)
		if err := tx.Model(&model.Subscription{}).Where("id = ?", sub.ID).UpdateColumn("retirement_retry_at", retry).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &sub.UserID, Actor: "system", Action: "subscription.retirement_deferred", Target: fmt.Sprintf("subscription:%d", sub.ID), Detail: fmt.Sprintf("reason=%s; epoch=%d; end_at=%s; retry_at=%s; financial closure rolled back; histories retained; access expiry is independently scheduled", reason, sub.EntitlementEpoch, sub.EndAt.UTC().Format(time.RFC3339Nano), retry.Format(time.RFC3339Nano))}).Error
	})
}

func equalRetirementTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
