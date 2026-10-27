package commercestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// The plan lock precedes these subscription locks, matching checkout's
// plan-before-entitlement order. No order or wallet facts are modified here.
func synchronizePlanTrafficQuota(tx *gorm.DB, plan commerce.Plan, actor uint, scope planSubscriptionSyncScope) (int, error) {
	now := scope.now
	count := 0
	var after uint
	for {
		subscriptions, err := scope.page(tx, after, false)
		if err != nil {
			return 0, err
		}
		if len(subscriptions) == 0 {
			break
		}
		after = subscriptions[len(subscriptions)-1].ID
		for _, sub := range subscriptions {
			// A legacy fixed-quota exhaustion may not have its terminal marker yet.
			if sub.EndsOnQuotaExhaustion && sub.FlowUsed >= sub.FlowTotal {
				continue
			}
			var baseSync *commerce.PerpetualBaseQuotaSyncDetail
			var historyBytes int64
			var next int64
			if sub.ResetQuotaBytes > 0 && sub.CycleStartUsed >= 0 && sub.FlowTotal >= sub.CycleStartUsed && sub.FlowTotal-sub.CycleStartUsed == sub.ResetQuotaBytes {
				// Preserve the established ordinary single-cycle quota rule.
				var err error
				next, err = commerce.SynchronizedPlanQuota(sub.CycleStartUsed, sub.FlowUsed, sub.FlowTotal, plan.TrafficBytes)
				if err != nil {
					return 0, err
				}
			} else {
				// A real base lot is resolved from immutable paid evidence and prior
				// non-payment events. Neither latest RQB nor FlowTotal can name it.
				detail, priorBytes, err := prepareMixedPlanBaseQuotaSync(tx, sub, plan, now)
				if err != nil {
					var invalid *commerce.ValidationError
					if errors.As(err, &invalid) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return 0, err
					}
					if !errors.Is(err, commerce.ErrValueEvidence) && !errors.Is(err, commerce.ErrSubscriptionValueUnavailable) && !errors.Is(err, commerce.ErrSubscriptionEvidenceBudget) && !errors.Is(err, commerce.ErrSubscriptionQuoteChanged) {
						return 0, err
					}
					return 0, &commerce.ValidationError{Message: "历史订阅基础额度与加购来源无法安全核对，套餐与订阅均未修改。", Fields: map[string]string{"sync_traffic_quota": "只同步已核实的基础批次；缺少可信历史时请通过工单核对，不会改写加购或付款依据。"}}
				}
				baseSync, next, historyBytes = &detail, detail.After.FlowTotal, priorBytes
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
			var detail []byte
			var err error
			if baseSync != nil {
				detail, err = json.Marshal(baseSync)
			} else {
				detail, err = json.Marshal(map[string]any{"plan_id": plan.ID, "plan_revision": plan.Revision, "actor_id": actor, "previous_flow_total": oldTotal, "flow_total": next, "flow_used": sub.FlowUsed, "previous_reset_quota_bytes": oldQuota, "reset_quota_bytes": plan.TrafficBytes})
			}
			if err != nil {
				return 0, err
			}
			event := model.QuotaEvent{EntitlementEpoch: sub.EntitlementEpoch, CreatedAt: now, SubscriptionID: sub.ID, EventType: "plan_quota_sync", DeltaBytes: next - oldTotal, BalanceBefore: before, BalanceAfter: max(int64(0), next-sub.FlowUsed), ReferenceType: "plan_revision", ReferenceID: fmt.Sprintf("%d:%d", plan.ID, plan.Revision), Detail: string(detail)}
			if err := tx.Create(&event).Error; err != nil {
				return 0, err
			}
			if baseSync != nil {
				if err := verifyStoredPlanBaseQuotaBudget(tx, event.ID, historyBytes); err != nil {
					return 0, err
				}
			}
			count++
		}
	}
	return count, nil
}
