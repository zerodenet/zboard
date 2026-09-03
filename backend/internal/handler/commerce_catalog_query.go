package handler

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
)

// An explicit operation selects a storefront projection, even for an admin
// browsing the public catalog. Omitted operation preserves the admin list.
type catalogScope struct {
	Operation     string
	PlanID        uint
	ExcludePlanID uint
}

func parseCatalogScope(values url.Values) (catalogScope, error) {
	scope := catalogScope{Operation: strings.TrimSpace(values.Get("operation"))}
	if scope.Operation != "" {
		if _, ok := skuOperationOrder[scope.Operation]; !ok {
			return scope, fmt.Errorf("invalid operation")
		}
	}
	var err error
	if scope.PlanID, err = positiveQueryID(values, "plan_id"); err != nil {
		return scope, err
	}
	scope.ExcludePlanID, err = positiveQueryID(values, "exclude_plan_id")
	return scope, err
}

func (scope catalogScope) apply(query *gorm.DB) *gorm.DB {
	if scope.PlanID > 0 {
		query = query.Where("plans.id = ?", scope.PlanID)
	}
	if scope.ExcludePlanID > 0 {
		query = query.Where("plans.id <> ?", scope.ExcludePlanID)
	}
	if scope.Operation != "" {
		query = query.Where("plans.is_active = ?", true).
			Where(`EXISTS (SELECT 1 FROM plan_skus
				JOIN plan_sku_operations ON plan_sku_operations.plan_sku_id = plan_skus.id
				WHERE plan_skus.plan_id = plans.id AND plan_skus.is_active = ?
				AND plan_sku_operations.operation = ?)`, true, scope.Operation)
	}
	return query
}

// Preserve the existing self-service management set, but filter it before
// COUNT/LIMIT instead of taking two truncated status pages in the browser.
// This is a read filter, not order authorization or a new entitlement policy.
func applySubscriptionManagementFilter(query *gorm.DB, purpose string, now time.Time) (*gorm.DB, error) {
	switch purpose {
	case "":
		return query, nil
	case "change", "addon":
		return applyEffectiveSubscriptionStatusFilter(query, subStatusActive, now), nil
	case "manage", "renew":
		permanentStart := time.Date(perpetualSubscriptionEnd.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
		return query.Where(`((subscriptions.status = ? AND subscriptions.end_at > ?
			AND subscriptions.flow_used < subscriptions.flow_total)
			OR (subscriptions.status IN ? AND subscriptions.end_at >= ?
			AND subscriptions.flow_used >= subscriptions.flow_total))`,
			subStatusActive, now, []string{subStatusActive, subStatusExpired}, permanentStart), nil
	default:
		return nil, fmt.Errorf("invalid eligible_for")
	}
}
