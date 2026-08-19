package handler

import (
	"testing"
	"time"
)

func TestParseFairUseObservationRangeUsesBoundedWindows(t *testing.T) {
	tests := []struct {
		raw          string
		wantName     string
		wantDuration time.Duration
		wantBucket   time.Duration
	}{
		{"", "1d", 24 * time.Hour, 5 * time.Minute},
		{"1d", "1d", 24 * time.Hour, 5 * time.Minute},
		{"3d", "3d", 3 * 24 * time.Hour, 15 * time.Minute},
		{"7d", "7d", 7 * 24 * time.Hour, time.Hour},
		{"15d", "15d", 15 * 24 * time.Hour, time.Hour},
	}
	for _, test := range tests {
		got, err := parseFairUseObservationRange(test.raw)
		if err != nil {
			t.Fatalf("range %q: %v", test.raw, err)
		}
		if got.Name != test.wantName || got.Duration != test.wantDuration || got.BucketDuration != test.wantBucket {
			t.Fatalf("range %q = %#v", test.raw, got)
		}
	}
	for _, raw := range []string{"30d", "12h", "all", "-1d"} {
		if _, err := parseFairUseObservationRange(raw); err == nil {
			t.Fatalf("range %q should be rejected", raw)
		}
	}
}

func TestFairUsePercentileUsesNearestRank(t *testing.T) {
	values := []int64{0, 0, 1, 4, 10}
	if got := fairUsePercentile(values, 0.50); got != 1 {
		t.Fatalf("p50 = %d, want 1", got)
	}
	if got := fairUsePercentile(values, 0.95); got != 10 {
		t.Fatalf("p95 = %d, want 10", got)
	}
	if got := fairUsePercentile(nil, 0.95); got != 0 {
		t.Fatalf("empty p95 = %d, want 0", got)
	}
}
