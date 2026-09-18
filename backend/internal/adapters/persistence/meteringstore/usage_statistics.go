package meteringstore

import (
	"database/sql"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
	"time"
)

// A statistics snapshot is independent of cursor/limit. Two reads share a
// transaction snapshot; page reads remain live and never pretend this count
// was recalculated for each cursor movement.
func LoadUsageStatistics(base *gorm.DB, bucket UsageBucketSpec, window UsageWindow) (metering.UsageStatistics, error) {
	result := metering.UsageStatistics{Bucket: bucket.Name, AsOf: time.Now().UTC()}
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
		groups := bucket.GroupSource(scoped.Session(&gorm.Session{}), window).Select("1").Group(bucket.Group())
		return tx.Session(&gorm.Session{NewDB: true}).Table("(?) AS traffic_usage_buckets", groups).Count(&result.Total).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}

// UsageStatisticsReader owns incremental-to-full fallback. Authorization must
// be performed by the calling capability before consulting display snapshots.
type UsageStatisticsReader struct {
	Cache       metering.StatisticsCache
	Incremental *IncrementalCache
}

func (s UsageStatisticsReader) Read(base *gorm.DB, bucket UsageBucketSpec, window UsageWindow) (metering.UsageStatistics, error) {
	key := UsageSnapshotQueryKey(base, bucket.Name)
	load := func() (metering.UsageStatistics, error) {
		if s.Incremental != nil {
			value, used, err := s.Incremental.Load(base, bucket, key)
			if err != nil || used {
				return value, err
			}
		}
		return LoadUsageStatistics(base, bucket, window)
	}
	if s.Cache == nil {
		return load()
	}
	return s.Cache.Get(base.Statement.Context, key, load)
}
