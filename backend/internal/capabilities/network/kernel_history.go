package network

import (
	"context"
	"errors"
)

var ErrKernelHistoryUnavailable = errors.New("kernel history capability unavailable")

type KernelHistoryResult struct {
	State      KernelState       `json:"state"`
	Operations []KernelOperation `json:"operations"`
}

type KernelHistoryRepository interface {
	ReadKernelHistory(context.Context, KernelDetectionRequest, int) (KernelHistoryResult, error)
}

type KernelHistory struct {
	Repository KernelHistoryRepository
	Limit      int
}

func (q KernelHistory) Get(ctx context.Context, request KernelDetectionRequest) (KernelHistoryResult, error) {
	if q.Repository == nil || request.NodeID == 0 || request.ActorID == 0 {
		return KernelHistoryResult{}, ErrKernelHistoryUnavailable
	}
	limit := q.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return q.Repository.ReadKernelHistory(ctx, request, limit)
}
