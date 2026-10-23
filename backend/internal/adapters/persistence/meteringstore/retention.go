package meteringstore

import (
	"context"
	"time"
)

// Evaluation events explain derived Fair Use state. Insertion must never run
// retention inside the evaluator's state/event transaction.
// Existing time indexes begin with subscription_id or event_type, so this
// global unlocked discovery can scan/sort beyond LIMIT. No schema change is
// introduced here; only candidate output, row locks and deletion are bounded.
func (s ObservationRetention) PruneEvaluationEvents(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	return s.prune(ctx, &EventRecord{}, "occurred_at", cutoff, limit)
}
