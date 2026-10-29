package meteringstore

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
)

// EnsureHourlyUsageReady checks only complete UTC hours selected by
// TrafficAggregateSource. Its precise partial-hour ledger edges are already
// reliable and do not depend on hourly projection completion.
func EnsureHourlyUsageReady(tx *gorm.DB, scope TrafficScope, from, to time.Time) error {
	return EnsureHourlyUsageWindowsReady(tx, scope, [][2]time.Time{{from, to}})
}

// EnsureHourlyUsageWindowsReady handles calendar days whose local midnight can
// split a UTC hour. Their interior edge hours use the existing exact ledger
// edges too; checking only a combined multi-day window would incorrectly wait
// on an hour which no day reads from the rollup. At most 366 ranges share one
// indexed LIMIT 1 probe, and contiguous complete-hour ranges are merged.
func EnsureHourlyUsageWindowsReady(tx *gorm.DB, scope TrafficScope, windows [][2]time.Time) error {
	if len(windows) > 366 {
		return fmt.Errorf("hourly readiness accepts at most 366 calendar windows")
	}
	ranges := make([][2]time.Time, 0, len(windows))
	for _, window := range windows {
		from, to := window[0], window[1]
		if !from.IsZero() {
			start := from.UTC().Truncate(time.Hour)
			if start.Before(from) {
				start = start.Add(time.Hour)
			}
			from = start
		}
		if !to.IsZero() {
			to = to.UTC().Truncate(time.Hour)
		}
		if !from.IsZero() && !to.IsZero() && !from.Before(to) {
			continue
		}
		ranges = append(ranges, [2]time.Time{from, to})
	}
	if len(ranges) == 0 {
		return nil
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i][0].Before(ranges[j][0]) })
	merged := ranges[:0]
	for _, window := range ranges {
		if len(merged) > 0 {
			last := &merged[len(merged)-1]
			if last[1].IsZero() || window[0].IsZero() || !last[1].Before(window[0]) {
				if window[1].IsZero() || (!last[1].IsZero() && window[1].After(last[1])) {
					last[1] = window[1]
				}
				continue
			}
		}
		merged = append(merged, window)
	}
	query := tx.Table("metering_usage_projection_pending").Where("hourly_pending = ?", true)
	for _, filter := range []struct {
		column string
		id     uint
	}{{"user_id", scope.UserID}, {"subscription_id", scope.SubscriptionID}, {"node_id", scope.NodeID}, {"protocol_endpoint_id", scope.ProtocolEndpointID}} {
		if filter.id > 0 {
			query = query.Where(filter.column+" = ?", filter.id)
		}
	}
	parts := make([]string, 0, len(merged))
	args := make([]any, 0, len(merged)*2)
	for _, window := range merged {
		switch {
		case window[0].IsZero() && window[1].IsZero():
			parts = nil
		case window[0].IsZero():
			parts = append(parts, "record_at < ?")
			args = append(args, window[1])
		case window[1].IsZero():
			parts = append(parts, "record_at >= ?")
			args = append(args, window[0])
		default:
			parts = append(parts, "(record_at >= ? AND record_at < ?)")
			args = append(args, window[0], window[1])
		}
	}
	if len(parts) > 0 {
		query = query.Where("("+strings.Join(parts, " OR ")+")", args...)
	}
	var id uint
	result := query.Select("traffic_record_id").Limit(1).Scan(&id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return metering.ErrUsageProjectionPending
	}
	return nil
}
