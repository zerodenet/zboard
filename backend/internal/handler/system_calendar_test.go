package handler

import (
	"net/url"
	"testing"
	"time"
)

func mustTestLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %s: %v", name, err)
	}
	return location
}

func TestParseTrafficTrendRangeInLocationUsesShanghaiCalendarDay(t *testing.T) {
	location := mustTestLocation(t, "Asia/Shanghai")
	values := url.Values{"from": {"2026-08-18"}, "to": {"2026-08-18"}}
	from, to, days, err := parseTrafficTrendRangeInLocation(values, time.Date(2026, 8, 18, 8, 0, 0, 0, time.UTC), location)
	if err != nil {
		t.Fatalf("parse range: %v", err)
	}
	if days != 1 {
		t.Fatalf("days = %d, want 1", days)
	}
	if got, want := from.UTC(), time.Date(2026, 8, 17, 16, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("from UTC = %s, want %s", got, want)
	}
	if got, want := to.AddDate(0, 0, 1).UTC(), time.Date(2026, 8, 18, 16, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("end UTC = %s, want %s", got, want)
	}
}

func TestSystemDayBucketsRespectDSTDayLength(t *testing.T) {
	location := mustTestLocation(t, "America/Los_Angeles")

	spring := time.Date(2026, 3, 8, 0, 0, 0, 0, location)
	springBucket := systemDayBuckets(spring, 1, location)[0]
	if got := springBucket.EndUTC.Sub(springBucket.StartUTC); got != 23*time.Hour {
		t.Fatalf("spring-forward day length = %s, want 23h", got)
	}

	fall := time.Date(2026, 11, 1, 0, 0, 0, 0, location)
	fallBucket := systemDayBuckets(fall, 1, location)[0]
	if got := fallBucket.EndUTC.Sub(fallBucket.StartUTC); got != 25*time.Hour {
		t.Fatalf("fall-back day length = %s, want 25h", got)
	}
}

func TestResolveDashboardPeriodInLocationUsesSystemMidnight(t *testing.T) {
	location := mustTestLocation(t, "Asia/Shanghai")
	now := time.Date(2026, 8, 18, 7, 30, 0, 0, time.UTC) // 15:30 Shanghai
	period, err := resolveDashboardPeriodInLocation(dashboardRangeToday, now, location)
	if err != nil {
		t.Fatalf("resolve period: %v", err)
	}
	if got, want := period.From, time.Date(2026, 8, 17, 16, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("from = %s, want %s", got, want)
	}
	if period.Timezone != "Asia/Shanghai" {
		t.Fatalf("timezone = %q, want Asia/Shanghai", period.Timezone)
	}
	if got, want := period.PreviousTo, time.Date(2026, 8, 17, 7, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("previous to = %s, want %s", got, want)
	}
}

func TestDashboardCalendarBucketsKeepRepeatedDSTHourDistinct(t *testing.T) {
	location := mustTestLocation(t, "America/Los_Angeles")
	period := dashboardPeriod{
		Range:    dashboardRangeToday,
		Bucket:   "hour",
		Timezone: location.String(),
		From:     time.Date(2026, 11, 1, 7, 0, 0, 0, time.UTC),
		To:       time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC),
	}
	buckets := dashboardCalendarBuckets(period, location)
	if len(buckets) != 25 {
		t.Fatalf("fall-back hourly bucket count = %d, want 25", len(buckets))
	}
	seen := make(map[string]struct{}, len(buckets))
	for _, bucket := range buckets {
		if _, exists := seen[bucket.Key]; exists {
			t.Fatalf("duplicate opaque bucket key %q", bucket.Key)
		}
		seen[bucket.Key] = struct{}{}
	}
}

func TestSystemBucketCaseExpressionUsesUTCBoundaries(t *testing.T) {
	location := mustTestLocation(t, "Asia/Shanghai")
	buckets := systemDayBuckets(time.Date(2026, 8, 18, 0, 0, 0, 0, location), 2, location)
	expression, args := systemBucketCaseExpression("record_at", buckets)
	if expression == "NULL" {
		t.Fatal("expected CASE expression")
	}
	if len(args) != 4 {
		t.Fatalf("argument count = %d, want 4", len(args))
	}
	if got, ok := args[0].(time.Time); !ok || !got.Equal(time.Date(2026, 8, 17, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("first boundary = %#v", args[0])
	}
}
