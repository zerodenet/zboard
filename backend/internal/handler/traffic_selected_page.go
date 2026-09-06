package handler

import (
	"strings"

	"gorm.io/gorm"
)

// SQLite's grouping index can identify the first page without reading byte
// columns from every record in the current bucket. Only those selected groups
// then fetch and aggregate their scoped records. Both phases share one SQL
// snapshot. The second source retains every original filter, including endpoint
// filters that are deliberately not part of the user-facing grouping key.
func (b trafficUsageBucketSpec) selectedFirstPageQuery(scoped, pageSource *gorm.DB, limit int) *gorm.DB {
	selected := pageSource.Session(&gorm.Session{}).
		Select("MIN(id) AS id, user_id, COALESCE(subscription_id, 0) AS subscription_id, node_id, protocol_multiplier_milli, " + b.Expression + " AS record_at").
		Group(b.group()).Order("record_at desc, id desc").Limit(limit + 1)
	rows := scoped.Session(&gorm.Session{}).Select("*")
	// CROSS JOIN keeps the bounded page as the outer loop. The matching bucket
	// and dimension equalities seek the existing index for each selected group.
	join := strings.ReplaceAll(b.Expression, "record_at", "t.record_at") + " = p.record_at" +
		" AND t.user_id IS p.user_id AND COALESCE(t.subscription_id, 0) = p.subscription_id" +
		" AND t.node_id IS p.node_id AND t.protocol_multiplier_milli IS p.protocol_multiplier_milli"
	aggregated := scoped.Session(&gorm.Session{NewDB: true}).Table("(?) AS p CROSS JOIN (?) AS t ON "+join, selected, rows).
		Select(`p.id, p.user_id, p.subscription_id, p.node_id, p.protocol_multiplier_milli,
COALESCE(SUM(t.raw_bytes), 0) AS raw_bytes, COALESCE(SUM(t.upload_bytes), 0) AS upload_bytes,
COALESCE(SUM(t.download_bytes), 0) AS download_bytes, COALESCE(SUM(t.used_bytes), 0) AS used_bytes,
p.record_at, COUNT(*) AS record_count`).Group("p.id")
	return scoped.Session(&gorm.Session{NewDB: true}).Table("(?) AS traffic_usage_buckets", aggregated)
}
