package commercestore

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const planGroupSyncLimit = 1000

// The plan's current group is the access policy. Replacing it has no per-
// subscription opt-out and never changes paid terms or historical order facts.
// Existing credential revocation records its old node routing before new-group
// publication is queued. The normal publication worker prepares new bindings.
func synchronizePlanGroup(tx *gorm.DB, plan commerce.Plan, actor uint) (int, error) {
	now := time.Now().UTC()
	grace, err := entitlementstore.RecoveryGracePeriod(tx)
	if err != nil {
		return 0, err
	}
	var rows []model.Subscription
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("plan_id = ? AND node_group_id <> ? AND status IN ?", plan.ID, plan.NodeGroupID, []string{"active", "expired"}).
		Where("(ended_at IS NULL AND end_at > ? AND (ends_on_quota_exhaustion = ? OR flow_used < flow_total)) OR (lifecycle IN ? AND COALESCE(ended_at,end_at) > ? AND end_reason <> ?)", now, false, []string{"renewable", ""}, now.Add(-grace), "canceled").
		Order("id ASC").Limit(planGroupSyncLimit + 1).Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) > planGroupSyncLimit {
		return 0, &commerce.ValidationError{Message: "更换接入组涉及超过 1000 份订阅，套餐与授权均未修改。", Fields: map[string]string{"node_group_id": "当前更换接入组仅支持最多 1000 份未结束订阅。"}}
	}
	if len(rows) != 0 {
		// Scope revocation locks old credentials. Current new-group endpoint
		// routing must be protected before those locks, matching endpoint edits.
		if _, err := networkstore.LockCurrentCredentialEndpointRoutes(tx, plan.NodeGroupID); err != nil {
			return 0, err
		}
	}
	for _, sub := range rows {
		previous := sub.NodeGroupID
		sub.NodeGroupID = plan.NodeGroupID
		if err := tx.Model(&sub).Select("node_group_id").Updates(&sub).Error; err != nil {
			return 0, err
		}
		if err := entitlementstore.RevokeOutsideGroup(tx, sub, now); err != nil {
			return 0, err
		}
		if err := networkstore.EnqueueSubscriptionPublications(tx, sub.ID, actor); err != nil {
			return 0, err
		}
		if err := tx.Create(&model.AuditLog{UserID: &actor, Actor: fmt.Sprintf("admin:%d", actor), Action: "subscription.plan_group.sync", Target: fmt.Sprintf("subscription:%d", sub.ID), Detail: fmt.Sprintf("plan=%d revision=%d previous_group=%d node_group=%d delivery=pending_verification", plan.ID, plan.Revision, previous, plan.NodeGroupID)}).Error; err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}
