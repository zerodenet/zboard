package meteringstore

import (
	"context"
	"database/sql"
	"math"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
)

func (s TrafficTrends) ReadUsers(ctx context.Context, actor uint, users []uint, q metering.TrafficTrendQuery) (metering.UserActivityData, error) {
	out := metering.UserActivityData{From: q.Buckets[0].Key, To: q.Buckets[len(q.Buckets)-1].Key, Timezone: q.Timezone, Items: make([]metering.UserActivityItem, 0, len(users))}
	if err := authorizeReportingRead(ctx, s.DB, actor, true); err != nil {
		return out, err
	}
	db := s.ReadDB
	if db == nil {
		db = s.DB
	}
	err := db.WithContext(ctx).Transaction(func(read *gorm.DB) error {
		windows := make([][2]time.Time, 0, len(q.Buckets))
		for _, b := range q.Buckets {
			windows = append(windows, [2]time.Time{b.StartUTC, b.EndUTC})
		}
		if _, err := inspectHourlyUsageWindowsForUsers(read, TrafficScope{}, windows, users); err != nil {
			return err
		}
		rows, err := loadUserTrafficActivity(read, users, q)
		if err != nil {
			return err
		}
		byUser := make(map[uint]map[string]int64, len(users))
		for _, row := range rows {
			if row.UsedBytes < 0 || len(row.Day) < 10 {
				return metering.ErrUsageProjectionPending
			}
			if byUser[row.UserID] == nil {
				byUser[row.UserID] = map[string]int64{}
			}
			byUser[row.UserID][row.Day[:10]] = row.UsedBytes
		}
		for _, id := range users {
			item := metering.UserActivityItem{UserID: id, Days: make([]metering.UserActivityDay, 0, len(q.Buckets))}
			for _, b := range q.Buckets {
				used := byUser[id][b.Key]
				if used > math.MaxInt64-item.UsedBytes {
					return metering.ErrUsageProjectionPending
				}
				item.UsedBytes += used
				item.Days = append(item.Days, metering.UserActivityDay{Date: b.Key, UsedBytes: used})
			}
			out.Items = append(out.Items, item)
		}
		out.AsOf = time.Now().UTC()
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return metering.UserActivityData{}, err
	}
	return out, nil
}

type userActivityRow struct {
	UserID    uint
	Day       string
	UsedBytes int64
}

// One statement for the entire page, including all subscriptions and exact
// calendar edges. No joins to live subscriptions, no writes or extra retention.
func loadUserTrafficActivity(db *gorm.DB, users []uint, q metering.TrafficTrendQuery) ([]userActivityRow, error) {
	parts := make([]string, 0, len(q.Buckets))
	args := make([]any, 0, len(q.Buckets))
	for _, b := range q.Buckets {
		source := trafficAggregateSourceForUsers(db, b.StartUTC, b.EndUTC, TrafficScope{}, users).
			Select("? AS day,user_id,COALESCE(SUM(used_bytes),0) AS used_bytes", b.Key).Group("user_id")
		parts = append(parts, "SELECT * FROM (?) AS activity_day")
		args = append(args, source)
	}
	var rows []userActivityRow
	err := db.Raw(strings.Join(parts, " UNION ALL "), args...).Scan(&rows).Error
	return rows, err
}
