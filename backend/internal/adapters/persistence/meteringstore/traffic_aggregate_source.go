package meteringstore

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

type TrafficScope struct{ UserID, SubscriptionID, NodeID, ProtocolEndpointID uint }

// Complete UTC hours use the independently maintained projection. Public reads
// check EnsureHourlyUsageReadable in the same snapshot before using this source.
// Exact pending increments overlay complete-hour projections in one snapshot;
// partial calendar edges read the ledger. Continuous ingress never has to drain.
func TrafficAggregateSource(db *gorm.DB, from, to time.Time, scope TrafficScope) *gorm.DB {
	start := from.UTC().Truncate(time.Hour)
	if start.Before(from) {
		start = start.Add(time.Hour)
	}
	end := to.UTC().Truncate(time.Hour)
	apply := func(query *gorm.DB) *gorm.DB {
		for _, filter := range []struct {
			column string
			id     uint
		}{
			{"user_id", scope.UserID}, {"subscription_id", scope.SubscriptionID},
			{"node_id", scope.NodeID}, {"protocol_endpoint_id", scope.ProtocolEndpointID},
		} {
			if filter.id > 0 {
				query = query.Where(filter.column+" = ?", filter.id)
			}
		}
		return query
	}
	columns := "record_at, user_id, subscription_id, node_id, protocol_endpoint_id, protocol_multiplier_milli, raw_bytes, upload_bytes, download_bytes, used_bytes"
	ledger := apply(db.Table("traffic_records").Select(columns+", record_count").Where("record_at >= ? AND record_at < ?", from, to))
	if !start.IsZero() && !end.IsZero() && !start.Before(end) {
		return db.Table("(?) AS traffic_aggregate", ledger)
	}
	hourly := apply(db.Table("traffic_usage_hourly").Select(columns + ", record_count").Where("record_count > 0"))
	if !start.IsZero() {
		hourly = hourly.Where("record_at >= ?", start.Format("2006-01-02 15:04:05"))
	}
	if !end.IsZero() {
		hourly = hourly.Where("record_at < ?", end.Format("2006-01-02 15:04:05"))
	}
	parts := []string{"?", "?"}
	queries := []interface{}{hourly, pendingHourlySource(db, start, end, scope)}
	if !from.IsZero() && from.Before(start) {
		parts = append(parts, "?")
		queries = append(queries, apply(db.Table("traffic_records").Select(columns+", record_count").Where("record_at >= ? AND record_at < ?", from, start)))
	}
	if !to.IsZero() && end.Before(to) {
		parts = append(parts, "?")
		queries = append(queries, apply(db.Table("traffic_records").Select(columns+", record_count").Where("record_at >= ? AND record_at < ?", end, to)))
	}
	return db.Table("("+strings.Join(parts, " UNION ALL ")+") AS traffic_aggregate", queries...)
}

// Only the bounded, independently owned checkpoint is joined to canonical IDs;
// this never rescans financial history. Legacy pending rows own their entire
// contribution; minute checkpoints own only the unprojected increment.
func pendingHourlySource(db *gorm.DB, from, to time.Time, scope TrafficScope) *gorm.DB {
	selected := []string{"r.record_at", "r.user_id", "r.subscription_id", "r.node_id", "r.protocol_endpoint_id", "r.protocol_multiplier_milli"}
	for _, column := range []string{"raw_bytes", "upload_bytes", "download_bytes", "used_bytes", "record_count"} {
		selected = append(selected, "CASE WHEN r.is_aggregate=1 THEN p.delta_"+column+" ELSE r."+column+" END AS "+column)
	}
	q := db.Table("metering_usage_projection_pending").Where("hourly_pending = ?", true).Limit(UsageProjectionBatchLimit)
	if !from.IsZero() {
		q = q.Where("record_at >= ?", from)
	}
	if !to.IsZero() {
		q = q.Where("record_at < ?", to)
	}
	for _, f := range []struct {
		column string
		id     uint
	}{{"user_id", scope.UserID}, {"subscription_id", scope.SubscriptionID}, {"node_id", scope.NodeID}, {"protocol_endpoint_id", scope.ProtocolEndpointID}} {
		if f.id > 0 {
			q = q.Where(f.column+" = ?", f.id)
		}
	}
	join := "JOIN traffic_records r ON r.id=p.traffic_record_id"
	if db.Dialector.Name() == "mysql" {
		join = "STRAIGHT_JOIN traffic_records r ON r.id=p.traffic_record_id"
	}
	return db.Table("(?) AS p", q).Joins(join).Select(strings.Join(selected, ","))
}
