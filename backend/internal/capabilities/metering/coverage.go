package metering

import (
	"strings"
	"time"
)

type CoverageFact struct {
	NodeID              uint
	CoreInstanceID      string
	LastSequence        uint64
	LastEventID         string
	ContinuousSinceAt   time.Time
	LastReceivedAt      time.Time
	LastEventOccurredAt time.Time
	LastGapFromSequence uint64
	LastGapToSequence   uint64
	LastGapAt           *time.Time
	GapCount            uint64
	UpdatedAt           time.Time
}

type CoverageNode struct {
	NodeID              uint       `json:"node_id"`
	State               string     `json:"state"`
	Reason              string     `json:"reason"`
	CoreInstanceID      string     `json:"core_instance_id,omitempty"`
	LastSequence        uint64     `json:"last_sequence,omitempty"`
	ContinuousSinceAt   *time.Time `json:"continuous_since_at,omitempty"`
	LastReceivedAt      *time.Time `json:"last_received_at,omitempty"`
	LastGapFromSequence uint64     `json:"last_gap_from_sequence,omitempty"`
	LastGapToSequence   uint64     `json:"last_gap_to_sequence,omitempty"`
	LastGapAt           *time.Time `json:"last_gap_at,omitempty"`
}

type CoverageSummary struct {
	State         string         `json:"state"`
	Reason        string         `json:"reason"`
	WindowSeconds int            `json:"window_seconds"`
	RequiredNodes int            `json:"required_nodes"`
	CompleteNodes int            `json:"complete_nodes"`
	Nodes         []CoverageNode `json:"nodes"`
}

func ClassifyCoverage(row CoverageFact, exists bool, cutoff, freshCutoff time.Time) (string, string) {
	if !exists || strings.TrimSpace(row.CoreInstanceID) == "" || row.LastSequence == 0 {
		return "unknown", "no_sequence_coverage"
	}
	if row.LastReceivedAt.Before(freshCutoff) {
		return "incomplete", "stale_connector_coverage"
	}
	if row.ContinuousSinceAt.After(cutoff) {
		if row.LastGapAt != nil && !row.LastGapAt.Before(cutoff) {
			return "incomplete", "sequence_gap_in_window"
		}
		return "unknown", "coverage_warming"
	}
	return "complete", "continuous_sequence_coverage"
}
