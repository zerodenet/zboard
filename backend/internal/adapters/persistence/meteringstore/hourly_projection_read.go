package meteringstore

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
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
	query, err := hourlyPendingQuery(tx, scope, windows)
	if err != nil || query == nil {
		return err
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

func hourlyPendingQuery(tx *gorm.DB, scope TrafficScope, windows [][2]time.Time) (*gorm.DB, error) {
	if len(windows) > 366 {
		return nil, fmt.Errorf("hourly readiness accepts at most 366 calendar windows")
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
		return nil, nil
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
	return query, nil
}

func EnsureHourlyUsageReadable(tx *gorm.DB, scope TrafficScope, from, to time.Time) error {
	return EnsureHourlyUsageWindowsReadable(tx, scope, [][2]time.Time{{from, to}})
}

// Charts may overlay at most one worker batch of exact outstanding increments.
// A large/corrupt backlog stays explicitly unknown instead of scanning history
// or returning incomplete totals. Call within the same snapshot as the source.
func EnsureHourlyUsageWindowsReadable(tx *gorm.DB, scope TrafficScope, windows [][2]time.Time) error {
	q, err := hourlyPendingQuery(tx, scope, windows)
	if err != nil || q == nil {
		return err
	}
	var pending []model.MeteringUsageProjectionPending
	if err := q.Limit(UsageProjectionBatchLimit + 1).Find(&pending).Error; err != nil {
		return err
	}
	if len(pending) > UsageProjectionBatchLimit {
		return metering.ErrUsageProjectionPending
	}
	if len(pending) == 0 {
		return nil
	}
	ids := make([]uint, len(pending))
	for i, p := range pending {
		ids[i] = p.TrafficRecordID
	}
	var records []model.TrafficRecord
	if err := tx.Select("id,user_id,subscription_id,node_id,protocol_endpoint_id,record_at,is_aggregate,record_count,raw_bytes,upload_bytes,download_bytes,used_bytes").Where("id IN ?", ids).Find(&records).Error; err != nil {
		return err
	}
	byID := make(map[uint]model.TrafficRecord, len(records))
	for _, r := range records {
		byID[r.ID] = r
	}
	for _, p := range pending {
		r, ok := byID[p.TrafficRecordID]
		if !ok || r.UserID != p.UserID || r.SubscriptionID != p.SubscriptionID || r.NodeID != p.NodeID || r.ProtocolEndpointID != p.ProtocolEndpointID || p.RecordAt == nil || !r.At.UTC().Truncate(time.Hour).Equal(p.RecordAt.UTC()) {
			return metering.ErrUsageProjectionPending
		}
		if r.IsAggregate {
			if p.DeltaRecordCount <= 0 || p.DeltaRecordCount > r.RecordCount || p.DeltaRawBytes < 0 || p.DeltaRawBytes > r.RawBytes || p.DeltaUploadBytes < 0 || p.DeltaUploadBytes > r.UploadBytes || p.DeltaDownloadBytes < 0 || p.DeltaDownloadBytes > r.DownloadBytes || p.DeltaUsedBytes < 0 || p.DeltaUsedBytes > r.UsedBytes {
				return metering.ErrUsageProjectionPending
			}
		} else if p.DeltaRecordCount != 0 || p.DeltaRawBytes != 0 || p.DeltaUploadBytes != 0 || p.DeltaDownloadBytes != 0 || p.DeltaUsedBytes != 0 {
			return metering.ErrUsageProjectionPending
		}
	}
	return nil
}
