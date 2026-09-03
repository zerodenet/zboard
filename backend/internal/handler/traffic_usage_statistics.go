package handler

import (
	"database/sql"
	"time"

	"gorm.io/gorm"
)

type trafficUsageStatistics struct {
	Total      int64                   `json:"total"`
	Aggregates trafficRecordAggregates `json:"aggregates"`
	Bucket     string                  `json:"bucket"`
	AsOf       time.Time               `json:"as_of"`
}

// A statistics snapshot is independent of cursor/limit. Two reads share a
// transaction snapshot; page reads remain live and never pretend this count
// was recalculated for each cursor movement.
func loadTrafficUsageStatistics(base *gorm.DB, bucket trafficUsageBucketSpec) (trafficUsageStatistics, error) {
	result := trafficUsageStatistics{Bucket: bucket.Name, AsOf: time.Now().UTC()}
	err := base.Transaction(func(tx *gorm.DB) error {
		scoped := tx.Session(&gorm.Session{})
		if err := scoped.Session(&gorm.Session{}).Select(`
   COALESCE(SUM(raw_bytes), 0) AS raw_bytes,
   COALESCE(SUM(used_bytes), 0) AS used_bytes,
   COUNT(DISTINCT user_id) AS user_count,
   COUNT(DISTINCT NULLIF(subscription_id, 0)) AS subscription_count,
   COUNT(DISTINCT node_id) AS node_count,
   COUNT(DISTINCT protocol_endpoint_id) AS protocol_endpoint_count
  `).Scan(&result.Aggregates).Error; err != nil {
			return err
		}
		groups := scoped.Session(&gorm.Session{}).Select("1").Group(bucket.group())
		return tx.Session(&gorm.Session{NewDB: true}).Table("(?) AS traffic_usage_buckets", groups).Count(&result.Total).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}

// Null means deliberately not calculated, never zero. Legacy requests still
// receive numeric totals and aggregates unless include_totals=false is used.
func trafficUsagePageData(rows []trafficUsageBucket, total *int64, offset, limit int, next, previous *string) map[string]interface{} {
	return map[string]interface{}{
		"items": rows, "total": total, "offset": offset, "limit": limit,
		"page": map[string]interface{}{"total": total, "offset": offset, "limit": limit, "next_cursor": next, "previous_cursor": previous},
	}
}
