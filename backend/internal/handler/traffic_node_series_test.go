package handler

import (
	"testing"
	"time"
)

func TestTrafficNodeSeriesWindowBoundsMinuteAndHourBuckets(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	minute, err := parseTrafficUsageBucket("minute")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTrafficNodeSeriesWindow(minute, historyWindow{From: from, To: from.Add(7 * 24 * time.Hour)}); err != nil {
		t.Fatalf("seven day minute window should be accepted: %v", err)
	}
	if err := validateTrafficNodeSeriesWindow(minute, historyWindow{From: from, To: from.Add(7*24*time.Hour + time.Second)}); err == nil {
		t.Fatal("minute series longer than seven days should be rejected")
	}

	hour, err := parseTrafficUsageBucket("hour")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTrafficNodeSeriesWindow(hour, historyWindow{From: from, To: from.Add(90 * 24 * time.Hour)}); err != nil {
		t.Fatalf("hour series should support long analysis windows: %v", err)
	}

	day, err := parseTrafficUsageBucket("day")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTrafficNodeSeriesWindow(day, historyWindow{From: from, To: from.Add(366 * 24 * time.Hour)}); err != nil {
		t.Fatalf("day series should support the complete history window: %v", err)
	}
}
