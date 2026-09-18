package observabilitystore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"gorm.io/gorm"
)

type Dashboard struct{ DB *gorm.DB }

type dashboardOrderAggregate struct {
	RevenueCents         int64
	PreviousRevenueCents int64
	PaidOrders           int64
	PreviousPaidOrders   int64
	NewOrders            int64
	RenewOrders          int64
	CurrencyCount        int64
	Currency             string
}

type dashboardSubscriptionAggregate struct {
	NewSubscriptions         int64
	PreviousNewSubscriptions int64
	ActiveSubscriptions      int64
	ExpiringWithin24Hours    int64
	ExpiringWithin3Days      int64
	ExpiringWithin7Days      int64
	QuotaExhausted           int64
}

type dashboardOperationalAggregate struct {
	TrafficBytes            int64
	NodesTotal              int64
	NodesEnabled            int64
	ConnectorOnline         int64
	SSHVerified             int64
	TrafficReady            int64
	ObservedScopes          int64
	ActiveSubscriptions     int64
	ActiveFlows             int64
	ProtocolEndpoints       int64
	ActiveProtocolEndpoints int64
	PublishedPlans          int64
	PendingTickets          int64
	UnresolvedDeployments   int64
}

type dashboardTrendRow struct {
	BucketStart  string
	RevenueCents int64
	PaidOrders   int64
	NewOrders    int64
	RenewOrders  int64
}

func (s Dashboard) LoadDashboardTotals(ctx context.Context, now time.Time) (out observability.DashboardTotals, err error) {
	cutoff := now.Add(-2 * time.Minute)
	err = s.DB.WithContext(ctx).Raw(`SELECT
 users_summary.users,
 users_summary.active_users,
 node_summary.nodes,
 node_summary.offline_nodes,
 node_summary.connector_online_nodes,
 node_summary.ssh_verified_nodes,
 node_summary.traffic_ready_nodes,
 plan_summary.plans,
 order_summary.orders,
 order_summary.paid_orders,
 order_summary.pending_orders,
 order_summary.failed_orders,
 order_summary.revenue_cents,
 subscription_summary.subscriptions,
 subscription_summary.active_subscriptions,
 subscription_summary.traffic_pool_bytes,
 endpoint_summary.protocol_endpoints,
 endpoint_summary.active_protocol_endpoints,
 ticket_summary.pending_tickets,
 task_summary.failed_tasks,
 deployment_summary.failed_deployments
FROM
 (SELECT COUNT(*) AS users,
   COALESCE(SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END),0) AS active_users
  FROM users) AS users_summary
CROSS JOIN
 (SELECT COUNT(*) AS nodes,
   COALESCE(SUM(CASE WHEN is_enabled = 1 AND (last_seen_at IS NULL OR last_seen_at < ?) THEN 1 ELSE 0 END),0) AS offline_nodes,
   COALESCE(SUM(CASE WHEN is_enabled = 1 AND connector_last_seen_at >= ? THEN 1 ELSE 0 END),0) AS connector_online_nodes,
   COALESCE(SUM(CASE WHEN ssh_verified_at IS NOT NULL AND ssh_host_key_fingerprint <> '' THEN 1 ELSE 0 END),0) AS ssh_verified_nodes,
   COALESCE(SUM(CASE WHEN traffic_secret_prefix <> '' AND traffic_secret_revoked_at IS NULL THEN 1 ELSE 0 END),0) AS traffic_ready_nodes
  FROM nodes) AS node_summary
CROSS JOIN
 (SELECT COALESCE(SUM(CASE WHEN is_active = 1 THEN 1 ELSE 0 END),0) AS plans FROM plans) AS plan_summary
CROSS JOIN
 (SELECT COUNT(*) AS orders,
   COALESCE(SUM(CASE WHEN status = 'paid' THEN 1 ELSE 0 END),0) AS paid_orders,
   COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END),0) AS pending_orders,
   COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END),0) AS failed_orders,
   COALESCE(SUM(CASE WHEN status = 'paid' THEN (CASE WHEN assigned_by > 0 OR paid_amount > 0 THEN paid_amount ELSE amount_cents END) - refund_amount ELSE 0 END),0) AS revenue_cents
  FROM orders) AS order_summary
CROSS JOIN
 (SELECT COUNT(*) AS subscriptions,
   COALESCE(SUM(CASE WHEN status = 'active' AND end_at > ? AND flow_used < flow_total THEN 1 ELSE 0 END),0) AS active_subscriptions,
   COALESCE(SUM(CASE WHEN status = 'active' AND end_at > ? AND flow_used < flow_total THEN flow_total - flow_used ELSE 0 END),0) AS traffic_pool_bytes
  FROM subscriptions) AS subscription_summary
CROSS JOIN
 (SELECT COUNT(*) AS protocol_endpoints,
   COALESCE(SUM(CASE WHEN is_active = 1 THEN 1 ELSE 0 END),0) AS active_protocol_endpoints
  FROM protocol_endpoints) AS endpoint_summary
CROSS JOIN
 (SELECT COALESCE(SUM(CASE WHEN status IN ('open','pending_admin') THEN 1 ELSE 0 END),0) AS pending_tickets FROM tickets) AS ticket_summary
CROSS JOIN
 (SELECT COALESCE(SUM(CASE WHEN status = 3 THEN 1 ELSE 0 END),0) AS failed_tasks FROM tasks) AS task_summary
CROSS JOIN
 (SELECT COUNT(*) AS failed_deployments FROM protocol_deployments
  WHERE id IN (SELECT MAX(id) FROM protocol_deployments GROUP BY protocol_endpoint_id) AND status = 'failed') AS deployment_summary`, cutoff, cutoff, now, now).Scan(&out).Error
	return out, err
}

func (s Dashboard) LoadDashboard(ctx context.Context, period observability.DashboardPeriod, now time.Time, buckets []observability.DashboardTrendBucket) (observability.DashboardSnapshot, error) {
	db := s.DB.WithContext(ctx)
	var orders dashboardOrderAggregate
	if err := db.Table("orders").Select(`
 COALESCE(SUM(CASE WHEN paid_at >= ? AND paid_at < ? THEN (CASE WHEN assigned_by > 0 OR paid_amount > 0 THEN paid_amount ELSE amount_cents END) - refund_amount ELSE 0 END),0) AS revenue_cents,
 COALESCE(SUM(CASE WHEN paid_at >= ? AND paid_at < ? THEN (CASE WHEN assigned_by > 0 OR paid_amount > 0 THEN paid_amount ELSE amount_cents END) - refund_amount ELSE 0 END),0) AS previous_revenue_cents,
 COALESCE(SUM(CASE WHEN paid_at >= ? AND paid_at < ? THEN 1 ELSE 0 END),0) AS paid_orders,
 COALESCE(SUM(CASE WHEN paid_at >= ? AND paid_at < ? THEN 1 ELSE 0 END),0) AS previous_paid_orders,
 COALESCE(SUM(CASE WHEN paid_at >= ? AND paid_at < ? AND order_type = 'new' THEN 1 ELSE 0 END),0) AS new_orders,
 COALESCE(SUM(CASE WHEN paid_at >= ? AND paid_at < ? AND order_type = 'renew' THEN 1 ELSE 0 END),0) AS renew_orders,
 COUNT(DISTINCT CASE WHEN TRIM(currency) <> '' THEN UPPER(TRIM(currency)) END) AS currency_count,
 COALESCE(MIN(CASE WHEN TRIM(currency) <> '' THEN UPPER(TRIM(currency)) END),'') AS currency`,
		period.From, period.To, period.PreviousFrom, period.PreviousTo,
		period.From, period.To, period.PreviousFrom, period.PreviousTo,
		period.From, period.To, period.From, period.To).
		Where("status = ? AND paid_at IS NOT NULL AND paid_at >= ? AND paid_at < ?", "paid", period.PreviousFrom, period.To).
		Scan(&orders).Error; err != nil {
		return observability.DashboardSnapshot{}, err
	}

	var subscriptions dashboardSubscriptionAggregate
	if err := db.Table("subscriptions").Select(`
 COALESCE(SUM(CASE WHEN created_at >= ? AND created_at < ? THEN 1 ELSE 0 END),0) AS new_subscriptions,
 COALESCE(SUM(CASE WHEN created_at >= ? AND created_at < ? THEN 1 ELSE 0 END),0) AS previous_new_subscriptions,
 COALESCE(SUM(CASE WHEN status = 'active' AND end_at > ? AND flow_used < flow_total THEN 1 ELSE 0 END),0) AS active_subscriptions,
 COALESCE(SUM(CASE WHEN status = 'active' AND end_at > ? AND end_at <= ? AND flow_used < flow_total THEN 1 ELSE 0 END),0) AS expiring_within24_hours,
 COALESCE(SUM(CASE WHEN status = 'active' AND end_at > ? AND end_at <= ? AND flow_used < flow_total THEN 1 ELSE 0 END),0) AS expiring_within3_days,
 COALESCE(SUM(CASE WHEN status = 'active' AND end_at > ? AND end_at <= ? AND flow_used < flow_total THEN 1 ELSE 0 END),0) AS expiring_within7_days,
 COALESCE(SUM(CASE WHEN end_at > ? AND flow_total > 0 AND flow_used >= flow_total AND status IN ('active','expired') THEN 1 ELSE 0 END),0) AS quota_exhausted`,
		period.From, period.To, period.PreviousFrom, period.PreviousTo,
		now, now, now.Add(24*time.Hour), now, now.Add(72*time.Hour), now, now.Add(7*24*time.Hour), now).
		Scan(&subscriptions).Error; err != nil {
		return observability.DashboardSnapshot{}, err
	}

	var operational dashboardOperationalAggregate
	if err := db.Raw(`SELECT
 (SELECT COALESCE(SUM(used_bytes),0) FROM traffic_records WHERE record_at >= ? AND record_at < ?) AS traffic_bytes,
 (SELECT COUNT(*) FROM nodes) AS nodes_total,
 (SELECT COUNT(*) FROM nodes WHERE is_enabled = ?) AS nodes_enabled,
 (SELECT COUNT(*) FROM nodes WHERE is_enabled = ? AND connector_last_seen_at >= ?) AS connector_online,
 (SELECT COUNT(*) FROM nodes WHERE ssh_verified_at IS NOT NULL AND ssh_host_key_fingerprint <> '') AS ssh_verified,
 (SELECT COUNT(*) FROM nodes WHERE traffic_secret_prefix <> '' AND traffic_secret_revoked_at IS NULL) AS traffic_ready,
 (SELECT COUNT(*) FROM principal_flow_scope_currents WHERE scope_type = ?) AS observed_scopes,
 (SELECT COUNT(*) FROM principal_flow_scope_currents WHERE scope_type = ? AND active_flows > 0) AS active_subscriptions,
 (SELECT COALESCE(SUM(active_flows),0) FROM principal_flow_scope_currents WHERE scope_type = ?) AS active_flows,
 (SELECT COUNT(*) FROM protocol_endpoints) AS protocol_endpoints,
 (SELECT COUNT(*) FROM protocol_endpoints WHERE is_active = ?) AS active_protocol_endpoints,
 (SELECT COUNT(*) FROM plans WHERE is_active = ?) AS published_plans,
 (SELECT COUNT(*) FROM tickets WHERE status IN (?,?)) AS pending_tickets,
 (SELECT COUNT(*) FROM protocol_deployments WHERE id IN (SELECT MAX(id) FROM protocol_deployments GROUP BY protocol_endpoint_id) AND status = ?) AS unresolved_deployments`,
		period.From, period.To, true, true, now.Add(-2*time.Minute),
		"subscription", "subscription", "subscription", true, true, "open", "pending_admin", "failed").Scan(&operational).Error; err != nil {
		return observability.DashboardSnapshot{}, err
	}

	trend, err := loadDashboardTrend(db, period, buckets)
	if err != nil {
		return observability.DashboardSnapshot{}, err
	}
	currency, mixed := orders.Currency, orders.CurrencyCount > 1
	if orders.CurrencyCount == 0 {
		currency = "CNY"
	} else if mixed {
		currency = ""
	}
	offline := operational.NodesEnabled - operational.ConnectorOnline
	if offline < 0 {
		offline = 0
	}
	result := observability.DashboardSnapshot{
		Business: observability.DashboardBusiness{
			RevenueCents: orders.RevenueCents, PreviousRevenueCents: orders.PreviousRevenueCents,
			PaidOrders: orders.PaidOrders, PreviousPaidOrders: orders.PreviousPaidOrders,
			NewOrders: orders.NewOrders, RenewOrders: orders.RenewOrders,
			NewSubscriptions: subscriptions.NewSubscriptions, PreviousNewSubscriptions: subscriptions.PreviousNewSubscriptions,
			ActiveSubscriptions: subscriptions.ActiveSubscriptions, ExpiringWithin3Days: subscriptions.ExpiringWithin3Days,
			Currency: currency, MixedCurrency: mixed,
		},
		Service:        observability.DashboardService{TrafficBytes: operational.TrafficBytes, OnlineNodes: operational.ConnectorOnline, EnabledNodes: operational.NodesEnabled},
		Subscriptions:  observability.DashboardSubscriptionHealth{ExpiringWithin24Hours: subscriptions.ExpiringWithin24Hours, ExpiringWithin3Days: subscriptions.ExpiringWithin3Days, ExpiringWithin7Days: subscriptions.ExpiringWithin7Days, QuotaExhausted: subscriptions.QuotaExhausted},
		Attention:      observability.DashboardAttention{OfflineNodes: offline, UnresolvedDeployments: operational.UnresolvedDeployments, PendingTickets: operational.PendingTickets},
		Infrastructure: observability.DashboardInfrastructure{NodesTotal: operational.NodesTotal, NodesEnabled: operational.NodesEnabled, ConnectorOnline: operational.ConnectorOnline, SSHVerified: operational.SSHVerified, TrafficReady: operational.TrafficReady, ProtocolEndpoints: operational.ProtocolEndpoints, ActiveProtocolEndpoints: operational.ActiveProtocolEndpoints, PublishedPlans: operational.PublishedPlans, UnresolvedDeployments: operational.UnresolvedDeployments},
		Coverage:       observability.DashboardCoverage{PrincipalFlows: operational.ObservedScopes > 0}, Trend: trend,
	}
	if result.Coverage.PrincipalFlows {
		activeSubscriptions, activeFlows := operational.ActiveSubscriptions, operational.ActiveFlows
		result.Service.ActiveSubscriptions, result.Service.ActiveFlows = &activeSubscriptions, &activeFlows
	}
	return result, nil
}

func loadDashboardTrend(db *gorm.DB, period observability.DashboardPeriod, buckets []observability.DashboardTrendBucket) ([]observability.DashboardTrendPoint, error) {
	if len(buckets) == 0 {
		return []observability.DashboardTrendPoint{}, nil
	}
	parts := make([]string, 0, len(buckets))
	args := make([]interface{}, 0, len(buckets)*2)
	for _, bucket := range buckets {
		parts = append(parts, fmt.Sprintf("WHEN paid_at >= ? AND paid_at < ? THEN '%s'", bucket.Key))
		args = append(args, bucket.StartUTC, bucket.EndUTC)
	}
	expression := "CASE " + strings.Join(parts, " ") + " END"
	var rows []dashboardTrendRow
	if err := db.Table("orders").Select(expression+` AS bucket_start,
 COALESCE(SUM((CASE WHEN assigned_by > 0 OR paid_amount > 0 THEN paid_amount ELSE amount_cents END) - refund_amount),0) AS revenue_cents,
 COUNT(*) AS paid_orders,
 COALESCE(SUM(CASE WHEN order_type = 'new' THEN 1 ELSE 0 END),0) AS new_orders,
 COALESCE(SUM(CASE WHEN order_type = 'renew' THEN 1 ELSE 0 END),0) AS renew_orders`, args...).
		Where("status = ? AND paid_at IS NOT NULL AND paid_at >= ? AND paid_at < ?", "paid", period.From, period.To).
		Group("bucket_start").Order("bucket_start ASC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	byBucket := make(map[string]dashboardTrendRow, len(rows))
	for _, row := range rows {
		byBucket[strings.TrimSpace(row.BucketStart)] = row
	}
	points := make([]observability.DashboardTrendPoint, 0, len(buckets))
	for _, bucket := range buckets {
		row := byBucket[bucket.Key]
		points = append(points, observability.DashboardTrendPoint{BucketStart: bucket.StartUTC, RevenueCents: row.RevenueCents, PaidOrders: row.PaidOrders, NewOrders: row.NewOrders, RenewOrders: row.RenewOrders})
	}
	return points, nil
}
