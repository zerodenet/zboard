package commercestore

import (
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// This bounds materialized rows and bind variables, not the number of rights
// that a plan revision can update. All pages retain one enclosing transaction.
const planSubscriptionSyncPageSize = 128

const planQuotaSyncPredicate = "ended_at IS NULL AND end_at > ? AND (status = ? OR (status = ? AND ends_on_quota_exhaustion = ? AND flow_used >= flow_total))"
const planGroupSyncPredicate = "node_group_id <> ? AND status IN ? AND ((ended_at IS NULL AND end_at > ? AND (ends_on_quota_exhaustion = ? OR flow_used < flow_total)) OR (lifecycle IN ? AND COALESCE(ended_at,end_at) > ? AND end_reason <> ?))"

type planSubscriptionSyncScope struct {
	planID, groupID uint
	now             time.Time
	grace           time.Duration
	group, quota    bool
	lastLockedID    uint
}

// The caller already holds the plan lock used by purchase/renewal/replacement.
// Lock the entire union of subscription authorities before taking endpoint,
// credential or publication locks. Locking just each next page after writing
// the preceding page would introduce a new reverse lock order.
func preparePlanSubscriptionSync(tx *gorm.DB, current model.Plan, next commerce.Plan, syncQuota bool) (planSubscriptionSyncScope, error) {
	scope := planSubscriptionSyncScope{planID: next.ID, groupID: next.NodeGroupID, now: time.Now().UTC(), group: current.NodeGroupID != next.NodeGroupID, quota: syncQuota}
	if !scope.group && !scope.quota {
		return scope, nil
	}
	if scope.group {
		var err error
		scope.grace, err = entitlementstore.RecoveryGracePeriod(tx)
		if err != nil {
			return scope, err
		}
	}
	for {
		query := tx.Model(&model.Subscription{}).Where("plan_id = ? AND id > ?", scope.planID, scope.lastLockedID)
		switch {
		case scope.group && scope.quota:
			args := append(scope.groupArguments(), scope.quotaArguments()...)
			query = query.Where("("+planGroupSyncPredicate+") OR ("+planQuotaSyncPredicate+")", args...)
		case scope.group:
			query = query.Where(planGroupSyncPredicate, scope.groupArguments()...)
		default:
			query = query.Where(planQuotaSyncPredicate, scope.quotaArguments()...)
		}
		var ids []uint
		if err := query.Clauses(clause.Locking{Strength: "UPDATE"}).Order("id ASC").Limit(planSubscriptionSyncPageSize).Pluck("id", &ids).Error; err != nil {
			return scope, err
		}
		if len(ids) == 0 {
			return scope, nil
		}
		scope.lastLockedID = ids[len(ids)-1]
	}
}

func (s planSubscriptionSyncScope) groupArguments() []any {
	return []any{s.groupID, []string{"active", "expired"}, s.now, false, []string{"renewable", ""}, s.now.Add(-s.grace), "canceled"}
}
func (s planSubscriptionSyncScope) quotaArguments() []any {
	return []any{s.now, "active", "expired", false}
}

func (s planSubscriptionSyncScope) page(tx *gorm.DB, after uint, group bool) ([]model.Subscription, error) {
	query := tx.Model(&model.Subscription{}).Clauses(clause.Locking{Strength: "UPDATE"}).Where("plan_id = ? AND id > ? AND id <= ?", s.planID, after, s.lastLockedID)
	if group {
		query = query.Where(planGroupSyncPredicate, s.groupArguments()...).Select("id", "node_group_id")
	} else {
		query = query.Where(planQuotaSyncPredicate, s.quotaArguments()...).Select("id", "user_id", "plan_id", "plan_sku_id", "node_group_id", "entitlement_epoch", "status", "start_at", "end_at", "ended_at", "end_reason", "ends_on_quota_exhaustion", "flow_used", "flow_total", "cycle_start_used", "reset_quota_bytes", "reset_policy", "next_reset_at")
	}
	var rows []model.Subscription
	err := query.Order("id ASC").Limit(planSubscriptionSyncPageSize).Find(&rows).Error
	return rows, err
}
