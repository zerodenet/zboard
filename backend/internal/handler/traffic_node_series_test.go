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
	if err := validateTrafficNodeSeriesWindow(minute, historyWindow{From: from, To: from.Add(24 * time.Hour)}, false); err != nil {
		t.Fatalf("one day unfiltered minute window should be accepted: %v", err)
	}
	if err := validateTrafficNodeSeriesWindow(minute, historyWindow{From: from, To: from.Add(24*time.Hour + time.Second)}, false); err == nil {
		t.Fatal("unfiltered minute series longer than one day should be rejected")
	}
	if err := validateTrafficNodeSeriesWindow(minute, historyWindow{From: from, To: from.Add(7 * 24 * time.Hour)}, true); err != nil {
		t.Fatalf("seven day node-filtered minute window should be accepted: %v", err)
	}

	hour, err := parseTrafficUsageBucket("hour")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTrafficNodeSeriesWindow(hour, historyWindow{From: from, To: from.Add(31 * 24 * time.Hour)}, false); err != nil {
		t.Fatalf("unfiltered hour series should support 31 days: %v", err)
	}
	if err := validateTrafficNodeSeriesWindow(hour, historyWindow{From: from, To: from.Add(32 * 24 * time.Hour)}, false); err == nil {
		t.Fatal("unfiltered hour series longer than 31 days should be rejected")
	}
	if err := validateTrafficNodeSeriesWindow(hour, historyWindow{From: from, To: from.Add(366 * 24 * time.Hour)}, true); err != nil {
		t.Fatalf("node-filtered hour series should support the complete history window: %v", err)
	}

	day, err := parseTrafficUsageBucket("day")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTrafficNodeSeriesWindow(day, historyWindow{From: from, To: from.Add(366 * 24 * time.Hour)}, false); err != nil {
		t.Fatalf("day series should support the complete history window: %v", err)
	}
}
