package handler

import (
	"testing"
	"time"
)

func TestClassifyFairUseCoverage(t *testing.T) {
	now := time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)
	cutoff := now.Add(-5 * time.Minute)
	freshCutoff := now.Add(-fairUseCoverageFreshness)
	gapAt := now.Add(-time.Minute)

	cases := []struct {
		name   string
		row    fairUseNodeCoverage
		exists bool
		state  string
		reason string
	}{
		{name: "no cursor", exists: false, state: "unknown", reason: "no_sequence_coverage"},
		{
			name:   "stale",
			exists: true,
			row:    fairUseNodeCoverage{CoreInstanceID: "core-a", LastSequence: 10, ContinuousSinceAt: now.Add(-10 * time.Minute), LastReceivedAt: now.Add(-time.Minute)},
			state:  "incomplete", reason: "stale_connector_coverage",
		},
		{
			name:   "gap in window",
			exists: true,
			row:    fairUseNodeCoverage{CoreInstanceID: "core-a", LastSequence: 20, ContinuousSinceAt: gapAt, LastReceivedAt: now, LastGapAt: &gapAt},
			state:  "incomplete", reason: "sequence_gap_in_window",
		},
		{
			name:   "warming after reconnect",
			exists: true,
			row:    fairUseNodeCoverage{CoreInstanceID: "core-a", LastSequence: 30, ContinuousSinceAt: now.Add(-time.Minute), LastReceivedAt: now},
			state:  "unknown", reason: "coverage_warming",
		},
		{
			name:   "complete",
			exists: true,
			row:    fairUseNodeCoverage{CoreInstanceID: "core-a", LastSequence: 40, ContinuousSinceAt: now.Add(-10 * time.Minute), LastReceivedAt: now},
			state:  "complete", reason: "continuous_sequence_coverage",
		},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			state, reason := classifyFairUseCoverage(item.row, item.exists, cutoff, freshCutoff)
			if state != item.state || reason != item.reason {
				t.Fatalf("got %s/%s, want %s/%s", state, reason, item.state, item.reason)
			}
		})
	}
}
