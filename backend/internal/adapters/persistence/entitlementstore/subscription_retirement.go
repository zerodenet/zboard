package entitlementstore

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func subscriptionLifecycleDue(query *gorm.DB, now time.Time) *gorm.DB {
	return query.Where(`status IN ('active', 'expired') AND (
  (ended_at IS NULL AND (end_at <= ? OR (ends_on_quota_exhaustion = ? AND flow_used >= flow_total)))
  OR (ended_at IS NOT NULL AND (lifecycle <> 'renewable' OR ended_at <= ?
    OR EXISTS (SELECT 1 FROM orders WHERE (orders.subscription_id = subscriptions.id OR orders.target_subscription_id = subscriptions.id) AND orders.subscription_ended_at IS NULL)
    OR EXISTS (SELECT 1 FROM protocol_credentials WHERE protocol_credentials.subscription_id = subscriptions.id AND protocol_credentials.status IN ('active', 'prepared')))))`, now, true, now.Add(-entitlements.RenewalGracePeriod))
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
	return tx.Model(&model.Order{}).Where("subscription_id = ? OR target_subscription_id = ?", sub.ID, sub.ID).
		Updates(map[string]any{"subscription_ended_at": ended, "subscription_end_reason": reason, "subscription_final_flow_total": total, "subscription_final_flow_used": used}).Error
}

// Snapshot and pending-order cancellation precede deletion. A cleared target FK
// must never turn a late paid renewal into an unrelated new subscription.
func retireSubscription(tx *gorm.DB, sub model.Subscription, now time.Time) error {
	if err := tx.Model(&model.Order{}).Where("target_subscription_id = ? AND status = 'pending'", sub.ID).
		Updates(map[string]any{"status": "canceled", "canceled_at": now, "updated_at": now, "failure_reason": "目标订阅已结束并清理，请新购套餐。"}).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.Order{}).Where("target_subscription_id = ?", sub.ID).Update("target_subscription_id", nil).Error; err != nil {
		return err
	}
	for _, record := range []any{&model.FlowUsage{}, &model.TrafficRecord{}, &model.QuotaEvent{}, &model.SubscriptionMutation{}, &model.SubscriptionMember{}, &model.SubscriptionToken{}, &model.ProtocolCredential{}} {
		if err := tx.Where("subscription_id = ?", sub.ID).Delete(record).Error; err != nil {
			return err
		}
	}
	if !datastore.IsSQLite(tx) {
		if err := tx.Exec("DELETE FROM traffic_usage_hourly WHERE subscription_id = ?", sub.ID).Error; err != nil {
			return err
		}
	}
	if err := tx.Create(&model.AuditLog{Actor: "system", Action: "subscription.cleanup", Target: fmt.Sprintf("subscription:%d", sub.ID), Detail: sub.EndReason}).Error; err != nil {
		return err
	}
	return tx.Delete(&sub).Error
}
