package commercestore

import (
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// The plan's current group is the access policy. Replacing it has no per-
// subscription opt-out and never changes paid terms or historical order facts.
// Existing credential revocation records its old node routing before new-group
// publication is queued. The normal publication worker prepares new bindings.
func synchronizePlanGroup(tx *gorm.DB, plan commerce.Plan, actor uint, scope planSubscriptionSyncScope) (int, error) {
	now := scope.now
	count := 0
	var after uint
	for {
		rows, err := scope.page(tx, after, true)
		if err != nil {
			return 0, err
		}
		if len(rows) == 0 {
			break
		}
		after = rows[len(rows)-1].ID
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
		count += len(rows)
	}
	return count, nil
}
