package metering

import (
	"context"
	"time"
)

const UserActivityDays = 7
const UserActivityBatchLimit = 100

type UserActivityDay struct {
	Date      string `json:"date"`
	UsedBytes int64  `json:"used_bytes,string"`
}
type UserActivityItem struct {
	UserID    uint              `json:"user_id"`
	UsedBytes int64             `json:"used_bytes,string"`
	Days      []UserActivityDay `json:"days"`
}
type UserActivityData struct {
	From              string             `json:"from"`
	To                string             `json:"to"`
	Timezone          string             `json:"timezone"`
	AsOf              time.Time          `json:"as_of"`
	ProjectionPending bool               `json:"projection_pending"`
	Items             []UserActivityItem `json:"items"`
}
type UserActivityRepository interface {
	ReadUsers(context.Context, uint, []uint, TrafficTrendQuery) (UserActivityData, error)
}
type UserActivity struct{ Repository UserActivityRepository }

// The fixed calendar and bounded page batch prevent arbitrary history scans.
func (s UserActivity) Read(ctx context.Context, actor uint, users []uint, location *time.Location, now time.Time) (UserActivityData, error) {
	if actor == 0 {
		return UserActivityData{}, ErrTrendPermission
	}
	if len(users) == 0 || len(users) > UserActivityBatchLimit {
		return UserActivityData{}, &PolicyValidation{Fields: map[string]string{"user_ids": "select 1..100 users"}}
	}
	seen := map[uint]bool{}
	ids := make([]uint, 0, len(users))
	for _, id := range users {
		if id == 0 {
			return UserActivityData{}, &PolicyValidation{Fields: map[string]string{"user_ids": "invalid user"}}
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	if location == nil {
		location = time.UTC
	}
	local := now.In(location)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	from := today.AddDate(0, 0, -UserActivityDays+1)
	q := TrafficTrendQuery{Administrative: true, From: from, Days: UserActivityDays, Timezone: location.String()}
	for i := 0; i < UserActivityDays; i++ {
		day := from.AddDate(0, 0, i)
		q.Buckets = append(q.Buckets, TrendBucket{Key: day.Format("2006-01-02"), StartUTC: day.UTC(), EndUTC: day.AddDate(0, 0, 1).UTC()})
	}
	return s.Repository.ReadUsers(ctx, actor, ids, q)
}
