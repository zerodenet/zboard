package handler

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNodePublishSchedulerCoalescesLatestPendingRequest(t *testing.T) {
	scheduler := newNodePublishScheduler()
	scheduler.enqueue(scheduledNodePublish{nodeID: 5, endpointID: 11, requestedBy: 1})
	scheduler.enqueue(scheduledNodePublish{nodeID: 5, endpointID: 12, requestedBy: 2})

	request, ok := scheduler.take()
	if !ok || request.nodeID != 5 || request.endpointID != 12 || request.requestedBy != 2 {
		t.Fatalf("unexpected coalesced request: %+v, ok=%t", request, ok)
	}
	scheduler.enqueue(scheduledNodePublish{nodeID: 5, endpointID: 13, requestedBy: 3})
	if _, ok := scheduler.take(); ok {
		t.Fatal("a running node must not be published concurrently")
	}
	scheduler.finish(5)
	request, ok = scheduler.take()
	if !ok || request.endpointID != 13 {
		t.Fatalf("pending rerun was lost: %+v, ok=%t", request, ok)
	}
}

func TestContextMutexCancelsLockWait(t *testing.T) {
	lock := newContextMutex()
	if err := lock.Lock(context.Background()); err != nil {
		t.Fatalf("take initial lock: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := lock.Lock(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock wait error = %v, want deadline exceeded", err)
	}
	lock.Unlock()
}
