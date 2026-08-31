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

func TestAdvanceFairUseCoverageFoldsContiguousBatchWithoutFalseGap(t *testing.T) {
	base := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	row, changed := advanceFairUseCoverage(fairUseNodeCoverage{}, false, 5, zeroEventEnvelope{
		EventID: "event-10", EventType: "flow.updated", CoreInstanceID: "core-a", Sequence: 10,
	}, base)
	if !changed || row.LastSequence != 10 || row.GapCount != 0 {
		t.Fatalf("initial coverage = %+v, changed=%t", row, changed)
	}
	for sequence := uint64(11); sequence <= 20; sequence++ {
		row, changed = advanceFairUseCoverage(row, true, 5, zeroEventEnvelope{
			EventID: "event", EventType: "flow.updated", CoreInstanceID: "core-a", Sequence: sequence,
		}, base.Add(time.Duration(sequence-10)*time.Millisecond))
		if !changed {
			t.Fatalf("sequence %d was not folded", sequence)
		}
	}
	if row.LastSequence != 20 || row.GapCount != 0 || row.LastGapAt != nil {
		t.Fatalf("contiguous batch created a false gap: %+v", row)
	}
}

func TestAdvanceFairUseCoveragePreservesRealGap(t *testing.T) {
	base := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	current := fairUseNodeCoverage{
		NodeID: 5, CoreInstanceID: "core-a", LastSequence: 10,
		ContinuousSinceAt: base.Add(-time.Minute), LastReceivedAt: base.Add(-time.Second),
	}
	next, changed := advanceFairUseCoverage(current, true, 5, zeroEventEnvelope{
		EventID: "event-14", EventType: "stats.sampled", CoreInstanceID: "core-a", Sequence: 14,
	}, base)
	if !changed || next.GapCount != 3 || next.LastGapFromSequence != 11 || next.LastGapToSequence != 13 || next.LastGapAt == nil {
		t.Fatalf("real gap was not retained: %+v", next)
	}
}
