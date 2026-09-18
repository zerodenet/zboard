package handler

import (
	"context"
	"strings"
	"time"
)

func (h *handlers) jobReadiness(id string) func(context.Context) (bool, error) {
	if id != "admin_tasks" && id != "event_consumer" && !strings.HasPrefix(id, "node_publish_") {
		return nil
	}
	return func(ctx context.Context) (bool, error) {
		if id == "event_consumer" {
			value, ok := zeroEventRuntimeRegistry.Load(h)
			if !ok {
				return false, nil
			}
			return value.(*zeroEventRuntime).spool.Status().PendingEvents > 0, nil
		}
		if id == "admin_tasks" {
			return h.services.BatchLifecycle().Pending(ctx)
		}
		return h.services.NodePublication(nil).Pending(ctx, time.Now().UTC())
	}
}
