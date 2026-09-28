package meteringstore

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

type TrafficScope struct{ UserID, SubscriptionID, NodeID, ProtocolEndpointID uint }

// Complete UTC hours use the transactionally maintained projection. At most
// two partial hours read ledger rows, preserving exact arbitrary/DST windows.
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
	ledger := apply(db.Table("traffic_records").Select(columns+", 1 AS record_count").Where("record_at >= ? AND record_at < ?", from, to))
	if !start.Before(end) {
		return db.Table("(?) AS traffic_aggregate", ledger)
	}
	hourly := apply(db.Table("traffic_usage_hourly").Select(columns+", record_count").Where("record_at >= ? AND record_at < ? AND record_count > 0", start.Format("2006-01-02 15:04:05"), end.Format("2006-01-02 15:04:05")))
	parts := []string{"?"}
	queries := []interface{}{hourly}
	if from.Before(start) {
		parts = append(parts, "?")
		queries = append(queries, apply(db.Table("traffic_records").Select(columns+", 1 AS record_count").Where("record_at >= ? AND record_at < ?", from, start)))
	}
	if end.Before(to) {
		parts = append(parts, "?")
		queries = append(queries, apply(db.Table("traffic_records").Select(columns+", 1 AS record_count").Where("record_at >= ? AND record_at < ?", end, to)))
	}
	return db.Table("("+strings.Join(parts, " UNION ALL ")+") AS traffic_aggregate", queries...)
}
