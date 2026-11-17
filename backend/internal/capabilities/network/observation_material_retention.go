package network

import (
	"context"
	"errors"
)

type ObservationMaterialRetentionResult struct {
	Inspected, Retired int
	Bytes              uint64
}
type ObservationMaterialRetentionRepository interface {
	HasPending(context.Context) (bool, error)
	Retire(context.Context, int, int64) (ObservationMaterialRetentionResult, error)
}
type ObservationMaterialRetention struct {
	Repository ObservationMaterialRetentionRepository
}

func (s ObservationMaterialRetention) HasPending(ctx context.Context) (bool, error) {
	if s.Repository == nil {
		return false, errors.New("observation material retention unavailable")
	}
	return s.Repository.HasPending(ctx)
}
func (s ObservationMaterialRetention) Retire(ctx context.Context, limit int, budget int64) (ObservationMaterialRetentionResult, error) {
	if s.Repository == nil {
		return ObservationMaterialRetentionResult{}, errors.New("observation material retention unavailable")
	}
	return s.Repository.Retire(ctx, limit, budget)
}
