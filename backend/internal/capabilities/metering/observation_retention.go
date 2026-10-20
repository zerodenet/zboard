package metering

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const ObservationRetentionHorizon = 15 * 24 * time.Hour
const ObservationRetentionBatchLimit = 1000

type ObservationRetentionResult struct {
	FlowStarts, EvaluationEvents int64
}

type ObservationRetentionRepository interface {
	PruneFlowStarts(context.Context, time.Time, int) (int64, error)
	PruneEvaluationEvents(context.Context, time.Time, int) (int64, error)
}

type ObservationRetention struct {
	Repository ObservationRetentionRepository
}

// Run performs one batch per diagnostic table, in independent transactions.
// No drain loop, business audit, settlement record, cursor or projection intent
// participates. A failure never skips the other table.
func (s ObservationRetention) Run(ctx context.Context, now time.Time) (ObservationRetentionResult, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cutoff := now.UTC().Add(-ObservationRetentionHorizon)
	var result ObservationRetentionResult
	var failures []error
	var err error
	result.FlowStarts, err = s.Repository.PruneFlowStarts(ctx, cutoff, ObservationRetentionBatchLimit)
	if err != nil {
		failures = append(failures, fmt.Errorf("flow-start observation retention: %w", err))
	}
	result.EvaluationEvents, err = s.Repository.PruneEvaluationEvents(ctx, cutoff, ObservationRetentionBatchLimit)
	if err != nil {
		failures = append(failures, fmt.Errorf("evaluation-event observation retention: %w", err))
	}
	return result, errors.Join(failures...)
}
