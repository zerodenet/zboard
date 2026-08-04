package handler

import (
	"testing"
	"time"
)

func TestNewProtocolEndpointMutationTimingSplitsServerPhases(t *testing.T) {
	start := time.Unix(0, 0)
	timing := newProtocolEndpointMutationTiming(
		start,
		start.Add(12*time.Millisecond),
		start.Add(32*time.Millisecond),
		start.Add(39*time.Millisecond),
		start.Add(44*time.Millisecond),
	)

	if timing.ValidationMS != 12 || timing.TransactionMS != 20 || timing.TaskEnqueueMS != 7 || timing.ResponsePreparationMS != 5 || timing.ServerTotalMS != 44 {
		t.Fatalf("unexpected timing split: %+v", timing)
	}
}

func TestProtocolEndpointElapsedMillisecondsDoesNotReturnNegativeDurations(t *testing.T) {
	now := time.Now()
	if got := protocolEndpointElapsedMilliseconds(now, now.Add(-time.Millisecond)); got != 0 {
		t.Fatalf("expected zero for reversed timestamps, got %d", got)
	}
}
