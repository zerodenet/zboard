package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestContinuationWakeInvalidatesEmptyProbeAndKeepsAdmission(t *testing.T) {
	store := &emptyRuntimeStore{}
	r := &Runtime{workWake: make(chan struct{})}
	gate := runtimeExecutionStore{Store: store, runtime: r}
	if _, err := gate.Claim(t.Context(), "worker", []string{"job"}, time.Second); !errors.Is(err, ErrEmpty) {
		t.Fatal(err)
	}
	notification := r.workWake
	r.wakeWorkers()
	select {
	case <-notification:
	default:
		t.Fatal("existing workers were not notified")
	}
	if _, err := gate.Claim(t.Context(), "worker", []string{"job"}, time.Second); !errors.Is(err, ErrEmpty) {
		t.Fatal(err)
	}
	// All workers share the newly established empty probe, even after a wake.
	for range 4 {
		_, _ = gate.Claim(context.Background(), "worker", []string{"job"}, time.Second)
	}
	store.mu.Lock()
	claims := store.claims
	store.mu.Unlock()
	if claims != 2 {
		t.Fatalf("wake bypassed durable admission or lost empty coalescing: %d", claims)
	}
	r.draining.Store(true)
	notification = r.workWake
	r.wakeWorkers()
	select {
	case <-notification:
		t.Fatal("draining runtime broadcast another admission wake")
	default:
	}
	if _, err := gate.Claim(t.Context(), "worker", []string{"job"}, time.Second); !errors.Is(err, ErrEmpty) {
		t.Fatal(err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.claims != 2 {
		t.Fatal("draining runtime queried durable admission")
	}
}
