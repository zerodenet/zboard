package handler

import (
	"testing"
	"time"
)

func TestResolveDashboardPeriodUsesExplicitUTCBoundaries(t *testing.T) {
	now := time.Date(2026, time.August, 14, 10, 30, 0, 0, time.UTC)

	today, err := resolveDashboardPeriod("today", now)
	if err != nil {
		t.Fatal(err)
	}
	if today.From != time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC) || today.Bucket != "hour" || today.Timezone != "UTC" {
		t.Fatalf("unexpected today period: %+v", today)
	}
	if today.PreviousFrom != time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC) || today.PreviousTo != time.Date(2026, time.August, 13, 10, 30, 0, 0, time.UTC) {
		t.Fatalf("today comparison must use the same elapsed portion of the previous day: %+v", today)
	}

	sevenDays, err := resolveDashboardPeriod("7d", now)
	if err != nil {
		t.Fatal(err)
	}
	if sevenDays.From != time.Date(2026, time.August, 8, 0, 0, 0, 0, time.UTC) || sevenDays.Bucket != "day" {
		t.Fatalf("unexpected 7d period: %+v", sevenDays)
	}
	if sevenDays.PreviousFrom != time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC) || sevenDays.PreviousTo != time.Date(2026, time.August, 7, 10, 30, 0, 0, time.UTC) {
		t.Fatalf("7d comparison must shift the selected calendar range back seven days: %+v", sevenDays)
	}
	if sevenDays.PreviousTo.Sub(sevenDays.PreviousFrom) != sevenDays.To.Sub(sevenDays.From) {
		t.Fatalf("previous 7d interval must match the selected interval duration: %+v", sevenDays)
	}
}

func TestResolveDashboardPeriodRejectsUnknownRange(t *testing.T) {
	if _, err := resolveDashboardPeriod("quarter", time.Now()); err == nil {
		t.Fatal("expected unknown range to be rejected")
	}
}

func TestBuildDashboardTrendPointsFillsMissingBuckets(t *testing.T) {
	period := dashboardPeriod{
		Range:    dashboardRangeToday,
		From:     time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC),
		To:       time.Date(2026, time.August, 14, 2, 45, 0, 0, time.UTC),
		Bucket:   "hour",
		Timezone: "UTC",
	}
	rows := []dashboardTrendRow{
		{BucketStart: "2026-08-14 01:00:00", RevenueCents: 1200, PaidOrders: 2, NewOrders: 1, RenewOrders: 1},
	}

	points := buildDashboardTrendPoints(period, rows)
	if len(points) != 3 {
		t.Fatalf("expected 3 hourly buckets through the current hour, got %d", len(points))
	}
	if points[0].PaidOrders != 0 || points[1].PaidOrders != 2 || points[2].PaidOrders != 0 {
		t.Fatalf("missing buckets must be represented as real zero-order buckets: %+v", points)
	}
	if points[1].RevenueCents != 1200 || points[1].NewOrders != 1 || points[1].RenewOrders != 1 {
		t.Fatalf("unexpected populated bucket: %+v", points[1])
	}
}
