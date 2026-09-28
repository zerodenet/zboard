package entitlementstore

import (
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

// These read projections describe saleable operations, not authorization grants.
// The same predicates filter candidates before count/pagination and populate UI hints.
func subscriptionOperationPredicate(operation string, now time.Time) (string, []any) {
	var lifecycle, catalog string
	var args []any
	switch operation {
	case "renew":
		lifecycle = "subscriptions.lifecycle = 'renewable' AND subscriptions.status IN ('active', 'expired') AND ((subscriptions.ended_at IS NULL AND subscriptions.end_at > ?) OR COALESCE(subscriptions.ended_at, subscriptions.end_at) > ?)"
		args = []any{now, now.Add(-entitlements.RenewalGracePeriod)}
		catalog = "operation_plan.id = subscriptions.plan_id AND operation_plan.is_renewable = ? AND operation_sku.entitlement_mode = 'plan'"
		args = append(args, true)
	case "change":
		lifecycle = "subscriptions.ended_at IS NULL AND (subscriptions.status = 'active' OR (subscriptions.status = 'expired' AND subscriptions.flow_total > 0 AND subscriptions.flow_used >= subscriptions.flow_total)) AND subscriptions.end_at > ? AND subscriptions.end_at < ? AND (subscriptions.ends_on_quota_exhaustion = ? OR subscriptions.flow_used < subscriptions.flow_total)"
		args = []any{now, entitlements.PerpetualEnd, false}
		catalog = "operation_plan.id <> subscriptions.plan_id AND operation_sku.entitlement_mode = 'plan' AND operation_sku.billing_unit <> 'once'"
	case "addon":
		lifecycle = "subscriptions.ended_at IS NULL AND (subscriptions.status = 'active' OR (subscriptions.status = 'expired' AND subscriptions.flow_used >= subscriptions.flow_total)) AND subscriptions.end_at > ?"
		args = []any{now}
		catalog = "operation_plan.id = subscriptions.plan_id AND operation_sku.entitlement_mode = 'traffic_addon'"
	case "reset":
		lifecycle = "subscriptions.ended_at IS NULL AND subscriptions.status IN ('active', 'expired') AND subscriptions.end_at > ? AND subscriptions.end_at < ? AND subscriptions.reset_quota_bytes > 0"
		args = []any{now, entitlements.PerpetualEnd}
		catalog = "operation_plan.id = subscriptions.plan_id AND operation_sku.entitlement_mode = 'traffic_reset'"
	default:
		return "", nil
	}
	predicate := "(" + lifecycle + ") AND EXISTS (SELECT 1 FROM plan_skus operation_sku JOIN plans operation_plan ON operation_plan.id = operation_sku.plan_id JOIN plan_sku_operations operation_entry ON operation_entry.plan_sku_id = operation_sku.id WHERE " + catalog + " AND operation_plan.is_active = ? AND operation_plan.archived_at IS NULL AND operation_sku.is_active = ? AND operation_sku.archived_at IS NULL AND operation_entry.operation = ?)"
	return "(" + predicate + ")", append(args, true, true, operation)
}

func subscriptionSummarySelect(now time.Time) (string, []any) {
	columns := []string{subscriptionSummaryColumns}
	args := []any{now, now}
	for _, operation := range []string{"renew", "change", "addon", "reset"} {
		predicate, values := subscriptionOperationPredicate(operation, now)
		columns = append(columns, "CASE WHEN "+predicate+" THEN 1 ELSE 0 END AS can_"+operation)
		args = append(args, values...)
	}
	return strings.Join(columns, ", "), args
}
