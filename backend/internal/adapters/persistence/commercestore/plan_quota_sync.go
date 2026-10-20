package commercestore

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const planQuotaSyncLimit = 1000

// The plan lock precedes these subscription locks, matching checkout's
// plan-before-entitlement order. No order or wallet facts are modified here.
func synchronizePlanTrafficQuota(tx *gorm.DB, plan commerce.Plan, actor uint) (int, error) {
	now := time.Now().UTC()
	var subscriptions []model.Subscription
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("plan_id = ? AND ended_at IS NULL AND end_at > ? AND (status = ? OR (status = ? AND ends_on_quota_exhaustion = ? AND flow_used >= flow_total))", plan.ID, now, "active", "expired", false).Order("id ASC").Limit(planQuotaSyncLimit + 1).Find(&subscriptions).Error; err != nil {
		return 0, err
	}
	if len(subscriptions) > planQuotaSyncLimit {
		return 0, &commerce.ValidationError{Message: "同步范围超过单次 1000 份订阅上限，套餐与订阅均未修改。", Fields: map[string]string{"sync_traffic_quota": "请关闭本次同步后保存套餐；超过 1000 份的同步需另行安排。"}}
	}
	count := 0
	for _, sub := range subscriptions {
		// A legacy fixed-quota exhaustion may not have its terminal marker yet.
		if sub.EndsOnQuotaExhaustion && sub.FlowUsed >= sub.FlowTotal {
			continue
		}
		// Add-ons and legacy quota adjustments share FlowTotal today. Do not
		// discard or reprice their rights by pretending the whole cycle is the
		// plan's base quota. A separate source allocation contract is required.
		if sub.ResetQuotaBytes <= 0 || sub.CycleStartUsed < 0 || sub.FlowTotal < sub.CycleStartUsed || sub.FlowTotal-sub.CycleStartUsed != sub.ResetQuotaBytes {
			return 0, &commerce.ValidationError{Message: "历史订阅包含加购、单独调整或缺失的套餐额度依据，套餐与订阅均未修改。", Fields: map[string]string{"sync_traffic_quota": "为保留已购权益，混合额度需核对来源后再同步；可关闭本次同步单独保存套餐。"}}
		}
		next, err := commerce.SynchronizedPlanQuota(sub.CycleStartUsed, sub.FlowUsed, sub.FlowTotal, plan.TrafficBytes)
		if err != nil {
			return 0, err
		}
		if next == sub.FlowTotal && sub.ResetQuotaBytes == plan.TrafficBytes {
			continue
		}
		before := max(int64(0), sub.FlowTotal-sub.FlowUsed)
		oldTotal, oldQuota := sub.FlowTotal, sub.ResetQuotaBytes
		sub.FlowTotal, sub.ResetQuotaBytes = next, plan.TrafficBytes
		if sub.Status == "expired" {
			sub.Status = "active"
		}
		if err := tx.Model(&sub).Select("flow_total", "reset_quota_bytes", "status").Updates(&sub).Error; err != nil {
			return 0, err
		}
		if !entitlements.AccessAvailable(entitlements.Subscription(sub), now) {
			if err := entitlementstore.ExpireSubscriptionCredentials(tx, sub.ID, now); err != nil {
				return 0, err
			}
		}
		if err := networkstore.EnqueueSubscriptionPublications(tx, sub.ID, actor); err != nil {
			return 0, err
		}
		detail, err := json.Marshal(map[string]any{"plan_id": plan.ID, "plan_revision": plan.Revision, "actor_id": actor, "previous_flow_total": oldTotal, "flow_total": next, "flow_used": sub.FlowUsed, "previous_reset_quota_bytes": oldQuota, "reset_quota_bytes": plan.TrafficBytes})
		if err != nil {
			return 0, err
		}
		if err := tx.Create(&model.QuotaEvent{SubscriptionID: sub.ID, EventType: "plan_quota_sync", DeltaBytes: next - oldTotal, BalanceBefore: before, BalanceAfter: max(int64(0), next-sub.FlowUsed), ReferenceType: "plan_revision", ReferenceID: fmt.Sprintf("%d:%d", plan.ID, plan.Revision), Detail: string(detail)}).Error; err != nil {
			return 0, err
		}
		count++
	}
	return count, nil
}
