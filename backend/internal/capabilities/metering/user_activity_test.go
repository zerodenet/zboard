package metering

import (
	"context"
	"testing"
	"time"
)

type activityCapture struct {
	ids   []uint
	q     TrafficTrendQuery
	calls int
}

func (c *activityCapture) ReadUsers(_ context.Context, _ uint, ids []uint, q TrafficTrendQuery) (UserActivityData, error) {
	c.ids = ids
	c.q = q
	c.calls++
	return UserActivityData{}, nil
}
func TestUserActivityBoundsCalendarAndDeduplicatesPage(t *testing.T) {
	repo := &activityCapture{}
	s := UserActivity{Repository: repo}
	zone, _ := time.LoadLocation("America/New_York")
	now := time.Date(2026, 11, 3, 9, 0, 0, 0, zone)
	if _, err := s.Read(context.Background(), 1, []uint{7, 8, 7}, zone, now); err != nil {
		t.Fatal(err)
	}
	if len(repo.ids) != 2 || len(repo.q.Buckets) != 7 || repo.q.Buckets[0].Key != "2026-10-28" || repo.q.Buckets[6].Key != "2026-11-03" {
		t.Fatal(repo)
	}
	if repo.q.Buckets[4].EndUTC.Sub(repo.q.Buckets[4].StartUTC) != 25*time.Hour {
		t.Fatal("DST day was flattened", repo.q.Buckets)
	}
	for _, ids := range [][]uint{nil, {0}, make([]uint, 101)} {
		if _, err := s.Read(context.Background(), 1, ids, zone, now); err == nil {
			t.Fatal("unbounded batch accepted")
		}
	}
	if _, err := s.Read(context.Background(), 0, []uint{1}, zone, now); err == nil {
		t.Fatal("unauthorized")
	}
	if repo.calls != 1 {
		t.Fatal("invalid queries hit storage", repo.calls)
	}
}
