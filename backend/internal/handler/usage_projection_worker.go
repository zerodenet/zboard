package handler

import (
	"context"
	"time"
)

func (h *handlers) StartUsageProjectionWorker() {
	if h == nil || h.services == nil {
		return
	}
	h.startScheduledJob("usage_projection", time.Second, func(ctx context.Context) error {
		_, err := h.services.UsageProjections.Drain(ctx, 200)
		return err
	})
}
