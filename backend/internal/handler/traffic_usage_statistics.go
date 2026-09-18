package handler

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"

	"gorm.io/gorm"
)

type trafficUsageStatistics = metering.UsageStatistics

func loadTrafficUsageStatistics(base *gorm.DB, bucket trafficUsageBucketSpec, window historyWindow) (trafficUsageStatistics, error) {
	return meteringstore.LoadUsageStatistics(base, meteringstore.UsageBucketSpec(bucket), meteringstore.UsageWindow{From: window.From, To: window.To})
}

type trafficStatisticsCacheAdapter struct {
	cache *trafficSnapshotCache[trafficUsageStatistics]
}

func (a trafficStatisticsCacheAdapter) Get(ctx context.Context, key [32]byte, load func() (metering.UsageStatistics, error)) (metering.UsageStatistics, error) {
	return a.cache.get(ctx, key, load)
}

// Null means deliberately not calculated, never zero. Legacy requests still
// receive numeric totals and aggregates unless include_totals=false is used.
func trafficUsagePageData(rows []trafficUsageBucket, total *int64, offset, limit int, next, previous *string) map[string]interface{} {
	return map[string]interface{}{
		"items": rows, "total": total, "offset": offset, "limit": limit,
		"page": map[string]interface{}{"total": total, "offset": offset, "limit": limit, "next_cursor": next, "previous_cursor": previous},
	}
}
