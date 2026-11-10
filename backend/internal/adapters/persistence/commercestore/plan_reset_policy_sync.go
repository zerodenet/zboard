package commercestore

import (
	"encoding/json"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// The shared scope has locked the complete authority union in ascending order.
// Reset scheduling does not resize rights or rewrite any paid/usage evidence.
func synchronizePlanResetPolicy(tx *gorm.DB, plan commerce.Plan, actor uint, scope planSubscriptionSyncScope) (int, error) {
	count := 0
	var after uint
	for {
		rows, err := scope.page(tx, after, false)
		if err != nil {
			return 0, err
		}
		if len(rows) == 0 {
			return count, nil
		}
		after = rows[len(rows)-1].ID
		for _, sub := range rows {
			if sub.EndsOnQuotaExhaustion || entitlements.IsPerpetualEnd(sub.EndAt) {
				continue
			}
			changed, err := entitlements.ChangeTrafficResetSchedule(entitlements.Subscription(sub), plan.ResetPolicy, scope.now)
			if err != nil {
				return 0, err
			}
			sameNext := sub.NextResetAt == nil && changed.NextResetAt == nil || sub.NextResetAt != nil && changed.NextResetAt != nil && sub.NextResetAt.Equal(*changed.NextResetAt)
			if sub.ResetPolicy == changed.ResetPolicy && sameNext {
				continue
			}
			detail, err := json.Marshal(map[string]any{"actor_id": actor, "plan_id": plan.ID, "plan_revision": plan.Revision, "before": map[string]any{"reset_policy": sub.ResetPolicy, "next_reset_at": sub.NextResetAt}, "after": map[string]any{"reset_policy": changed.ResetPolicy, "next_reset_at": changed.NextResetAt}})
			if err != nil {
				return 0, err
			}
			if err := tx.Model(&sub).Updates(map[string]any{"reset_policy": changed.ResetPolicy, "next_reset_at": changed.NextResetAt, "updated_at": scope.now}).Error; err != nil {
				return 0, err
			}
			balance := sub.FlowTotal - sub.FlowUsed
			if err := tx.Create(&model.QuotaEvent{SubscriptionID: sub.ID, EntitlementEpoch: sub.EntitlementEpoch, CreatedAt: scope.now, EventType: "reset_policy_sync", BalanceBefore: balance, BalanceAfter: balance, ReferenceType: "plan_revision", ReferenceID: fmt.Sprintf("%d:%d", plan.ID, plan.Revision), Detail: string(detail)}).Error; err != nil {
				return 0, err
			}
			count++
		}
	}
}
