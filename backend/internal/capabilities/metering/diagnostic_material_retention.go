package metering

import (
	"context"
	"errors"
)

type DiagnosticMaterialRetentionResult struct {
	Inspected, Retired int
	Bytes              uint64
}

type DiagnosticMaterialRetentionRepository interface {
	HasPending(context.Context) (bool, error)
	Retire(context.Context, int, int64) (DiagnosticMaterialRetentionResult, error)
}

// This auxiliary capability retires proven optional bodies, never accounting
// facts, cumulative cursors, source identities or native rounding evidence.
type DiagnosticMaterialRetention struct {
	Repository DiagnosticMaterialRetentionRepository
}

func (s DiagnosticMaterialRetention) HasPending(ctx context.Context) (bool, error) {
	if s.Repository == nil {
		return false, errors.New("diagnostic retention unavailable")
	}
	return s.Repository.HasPending(ctx)
}

func (s DiagnosticMaterialRetention) Retire(ctx context.Context, limit int, budget int64) (DiagnosticMaterialRetentionResult, error) {
	if s.Repository == nil {
		return DiagnosticMaterialRetentionResult{}, errors.New("diagnostic retention unavailable")
	}
	return s.Repository.Retire(ctx, limit, budget)
}
