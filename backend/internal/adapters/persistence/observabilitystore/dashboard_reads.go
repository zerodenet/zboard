package observabilitystore

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"gorm.io/gorm"
	"time"
)

// Current state deliberately uses the authority pool and never reads history.
func loadDashboardCurrentOperations(db *gorm.DB, now time.Time, operational *dashboardOperationalAggregate) error {
	if err := db.Raw(`SELECT
 (SELECT COUNT(*) FROM nodes) AS nodes_total,
 (SELECT COUNT(*) FROM nodes WHERE is_enabled = ?) AS nodes_enabled,
 (SELECT COUNT(*) FROM nodes WHERE is_enabled = ? AND connector_last_seen_at >= ?) AS connector_online,
 (SELECT COUNT(*) FROM nodes WHERE ssh_verified_at IS NOT NULL AND ssh_host_key_fingerprint <> '') AS ssh_verified,
 (SELECT COUNT(*) FROM nodes WHERE traffic_secret_prefix <> '' AND traffic_secret_revoked_at IS NULL) AS traffic_ready,
 (SELECT COUNT(*) FROM principal_flow_scope_currents WHERE scope_type = ?) AS observed_scopes,
 (SELECT COUNT(*) FROM principal_flow_scope_currents WHERE scope_type = ? AND active_flows > 0) AS active_subscriptions,
 (SELECT COALESCE(SUM(active_flows),0) FROM principal_flow_scope_currents WHERE scope_type = ?) AS active_flows,
 (SELECT COUNT(*) FROM protocol_endpoints WHERE deleted_at IS NULL AND node_id NOT IN (SELECT id FROM nodes WHERE lifecycle_status = 'archived')) AS protocol_endpoints,
 (SELECT COUNT(*) FROM protocol_endpoints WHERE deleted_at IS NULL AND is_active = ?) AS active_protocol_endpoints,
 (SELECT COUNT(*) FROM plans WHERE is_active = ?) AS published_plans,
 (SELECT COUNT(*) FROM tickets WHERE status IN (?,?)) AS pending_tickets,
 (SELECT COUNT(*) FROM protocol_deployments WHERE id IN (SELECT MAX(id) FROM protocol_deployments GROUP BY protocol_endpoint_id) AND status = ?) AS unresolved_deployments`,
		true, true, now.Add(-2*time.Minute),
		"subscription", "subscription", "subscription", true, true, "open", "pending_admin", "failed").Scan(operational).Error; err != nil {
		return err
	}

	return nil
}

func loadDashboardSubscriptionHealth(db *gorm.DB, now time.Time, out *dashboardSubscriptionAggregate) error {
	return db.Table("subscriptions").Select(`
 COALESCE(SUM(CASE WHEN status = 'active' AND end_at > ? AND flow_used < flow_total THEN 1 ELSE 0 END),0) AS active_subscriptions,
 COALESCE(SUM(CASE WHEN status = 'active' AND end_at > ? AND end_at <= ? AND flow_used < flow_total THEN 1 ELSE 0 END),0) AS expiring_within24_hours,
 COALESCE(SUM(CASE WHEN status = 'active' AND end_at > ? AND end_at <= ? AND flow_used < flow_total THEN 1 ELSE 0 END),0) AS expiring_within3_days,
 COALESCE(SUM(CASE WHEN status = 'active' AND end_at > ? AND end_at <= ? AND flow_used < flow_total THEN 1 ELSE 0 END),0) AS expiring_within7_days,
 COALESCE(SUM(CASE WHEN end_at > ? AND flow_total > 0 AND flow_used >= flow_total AND status IN ('active','expired') THEN 1 ELSE 0 END),0) AS quota_exhausted`,
		now, now, now.Add(24*time.Hour), now, now.Add(72*time.Hour), now, now.Add(7*24*time.Hour), now).Where("end_at > ? AND status IN (?, ?)", now, "active", "expired").Scan(out).Error
}

func loadDashboardPeriodOperations(db *gorm.DB, period observability.DashboardPeriod, out *dashboardOperationalAggregate) error {
	traffic := meteringstore.TrafficAggregateSource(db, period.From, period.To, meteringstore.TrafficScope{}).Select("COALESCE(SUM(used_bytes), 0)")
	previous := meteringstore.TrafficAggregateSource(db, period.PreviousFrom, period.PreviousTo, meteringstore.TrafficScope{}).Select("COALESCE(SUM(used_bytes), 0)")
	return db.Raw(`SELECT (?) AS traffic_bytes, (?) AS previous_traffic_bytes,
 (SELECT COUNT(*) FROM users WHERE created_at >= ? AND created_at < ?) AS new_users,
 (SELECT COUNT(*) FROM users WHERE created_at >= ? AND created_at < ?) AS previous_new_users`,
		traffic, previous, period.From, period.To, period.PreviousFrom, period.PreviousTo).Scan(out).Error
}
