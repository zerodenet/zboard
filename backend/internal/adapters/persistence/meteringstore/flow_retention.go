package meteringstore

import (
	"context"
	"time"
)

// Flow starts are receive-time diagnostics, separate from TrafficRecord/FlowUsage
// settlement identities. Diagnostic deduplication lasts 24 hours; financial report
// identities and cumulative checkpoints remain independent and do not expire.
func (s ObservationRetention) PruneFlowStarts(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	return s.pruneProjectedFlowStarts(ctx, cutoff, limit)
}
