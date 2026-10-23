package entitlementstore

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func subscriptionLifecycleDue(query *gorm.DB, now time.Time, configuredGrace ...time.Duration) *gorm.DB {
	grace := entitlements.RenewalGracePeriod
	if len(configuredGrace) > 0 {
		grace = configuredGrace[0]
	}
	return query.Where(`status IN ('active', 'expired') AND (
  (ended_at IS NULL AND (end_at <= ? OR (ends_on_quota_exhaustion = ? AND flow_used >= flow_total)))
  OR (ended_at IS NOT NULL AND (lifecycle <> 'renewable' OR ended_at <= ?
    OR EXISTS (SELECT 1 FROM orders WHERE (orders.subscription_id = subscriptions.id OR orders.target_subscription_id = subscriptions.id) AND orders.subscription_ended_at IS NULL)
    OR EXISTS (SELECT 1 FROM protocol_credentials WHERE protocol_credentials.subscription_id = subscriptions.id AND protocol_credentials.status IN ('active', 'prepared')))))`, now, true, now.Add(-grace))
}

func endSubscription(tx *gorm.DB, sub *model.Subscription, now time.Time) error {
	ended, reason := sub.EndAt, "expired"
	if sub.EndedAt != nil {
		ended, reason = *sub.EndedAt, sub.EndReason
	}
	if sub.EndedAt == nil && sub.EndAt.After(now) {
		ended, reason = now, "exhausted"
	}
	sub.Status, sub.EndedAt, sub.EndReason = "expired", &ended, reason
	if err := tx.Model(sub).Updates(map[string]any{"status": sub.Status, "ended_at": ended, "end_reason": reason, "updated_at": now}).Error; err != nil {
		return err
	}
	total, used := entitlements.CycleQuota(entitlements.Subscription(*sub))
	// Settlement owns the affected orders before it owns the subscription. Read
	// their identities, then update by primary key: an OR-based UPDATE can scan
	// and lock unrelated orders in MySQL, reversing that established lock order.
	var orderIDs []uint
	if err := tx.Model(&model.Order{}).Where("(subscription_id = ? OR target_subscription_id = ?) AND subscription_ended_at IS NULL", sub.ID, sub.ID).
		Order("id").Pluck("id", &orderIDs).Error; err != nil {
		return err
	}
	if len(orderIDs) == 0 {
		return nil
	}
	return tx.Model(&model.Order{}).Where("id IN ?", orderIDs).
		Updates(map[string]any{"subscription_ended_at": ended, "subscription_end_reason": reason, "subscription_final_flow_total": total, "subscription_final_flow_used": used}).Error
}

// Retire a business instance without deleting identities needed for late
// metering/replay or financial reconciliation. Credentials are unusable, while
// their stable attribution keys and cumulative cursors remain available.
func retireSubscription(tx *gorm.DB, sub model.Subscription, cipher entitlements.AccessCipher, now time.Time) error {
	if cipher == nil {
		return entitlements.ErrCredentialExpiryUnavailable
	}
	var credentials []model.ProtocolCredential
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("subscription_id = ?", sub.ID).Find(&credentials).Error; err != nil {
		return err
	}
	for _, credential := range credentials {
		hash := credential.RetiredPrincipalHash
		if credential.Secret != "" {
			plain, err := cipher.Decrypt(credential.Secret)
			if err != nil {
				return fmt.Errorf("retire credential %d: %w", credential.ID, err)
			}
			hash = entitlements.RetiredPrincipalHash(credential.NodeID, plain)
		}
		if err := tx.Model(&model.ProtocolCredential{}).Where("id = ?", credential.ID).Updates(map[string]any{"retired_principal_hash": hash}).Error; err != nil {
			return err
		}
	}
	if err := tx.Model(&model.ProtocolCredential{}).Where("subscription_id = ?", sub.ID).Updates(map[string]any{"status": "revoked", "revoked_at": now, "secret": "", "updated_at": now}).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.SubscriptionToken{}).Where("subscription_id = ?", sub.ID).Updates(map[string]any{"revoked_at": now, "token_ciphertext": "", "updated_at": now}).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.Subscription{}).Where("id = ?", sub.ID).Updates(map[string]any{"status": "ended", "next_reset_at": nil, "updated_at": now}).Error; err != nil {
		return err
	}
	return tx.Create(&model.AuditLog{UserID: &sub.UserID, Actor: "system", Action: "subscription.cleanup", Target: fmt.Sprintf("subscription:%d", sub.ID), Detail: sub.EndReason + "; historical identities, cursors and accounting retained"}).Error
}
