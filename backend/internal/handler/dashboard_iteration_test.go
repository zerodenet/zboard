package handler

import (
	"testing"
	"time"
)

func TestDashboardCalendarMonthUsesSameLocalDateAndClampsShortPreviousMonth(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ now, previousTo string }{
		{"2026-03-28T13:30:00+08:00", "2026-02-28T13:30:00+08:00"},
		{"2026-03-31T13:30:00+08:00", "2026-03-01T00:00:00+08:00"},
		{"2024-03-29T13:30:00+08:00", "2024-02-29T13:30:00+08:00"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		want, _ := time.Parse(time.RFC3339, tc.previousTo)
		period, err := resolveDashboardPeriodInLocation("month", now, location)
		if err != nil {
			t.Fatal(err)
		}
		if period.From.In(location).Day() != 1 || period.PreviousFrom.In(location).Day() != 1 || period.Timezone != "Asia/Shanghai" || !period.PreviousTo.Equal(want) {
			t.Fatalf("period %+v want previousTo %s", period, want)
		}
		if period.PreviousTo.After(period.From) {
			t.Fatal("previous month comparison leaked into current month")
		}
		buckets := dashboardCalendarBuckets(period, location)
		if len(buckets) != now.In(location).Day() {
			t.Fatalf("month buckets %d", len(buckets))
		}
	}
}
