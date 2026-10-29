package meteringstore

import (
	"context"
	"time"
)

// Flow starts are receive-time diagnostics, separate from TrafficRecord/FlowUsage
// settlement identities. The existing fifteen-day horizon also bounds diagnostic
// (node_id, event_id) deduplication: an old replay may be observed again after pruning.
func (s ObservationRetention) PruneFlowStarts(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	return s.pruneProjectedFlowStarts(ctx, cutoff, limit)
}
