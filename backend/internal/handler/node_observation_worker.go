package handler

import (
	"context"
	"time"
)

func (h *handlers) StartNodeObservationWorker() {
	h.startScheduledJob("node_observation", time.Second, func(ctx context.Context) error {
		_, err := h.services.NodeObservations.Drain(ctx, 32, 8*1024*1024)
		return err
	})
}

// This is an explicit global administrative stop. Process shutdown uses the
// existing local runtime Drain/Close path instead.
func (h *handlers) CloseNodeObservationWorker() { h.closeScheduledJob("node_observation") }
