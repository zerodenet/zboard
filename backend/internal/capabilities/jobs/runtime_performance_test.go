package jobs

import (
	"context"
	"sync"
	"testing"
	"time"
)

type emptyRuntimeStore struct {
	mu     sync.Mutex
	claims int
}

func (s *emptyRuntimeStore) Submit(context.Context, Submission) (Run, error) {
	return Run{}, ErrInvalid
}
func (s *emptyRuntimeStore) Claim(context.Context, string, []string, time.Duration) (Claim, error) {
	s.mu.Lock()
	s.claims++
	s.mu.Unlock()
	return Claim{}, ErrEmpty
}
func (s *emptyRuntimeStore) Finish(context.Context, Claim, State) error { return nil }
func (s *emptyRuntimeStore) List(context.Context, string, int, int) ([]Run, error) {
	return nil, nil
}

func TestEmptyRuntimeClaimsAreCoalescedAcrossWorkers(t *testing.T) {
	store := &emptyRuntimeStore{}
	runtime := &Runtime{}
	gate := runtimeExecutionStore{Store: store, runtime: runtime}
	for i := 0; i < 4; i++ {
		if _, err := gate.Claim(context.Background(), "worker", []string{"job"}, time.Second); err != ErrEmpty {
			t.Fatal(err)
		}
	}
	store.mu.Lock()
	claims := store.claims
	store.mu.Unlock()
	if claims != 1 {
		t.Fatalf("empty store queried %d times, want one coalesced probe", claims)
	}
	runtime.claimMu.Lock()
	runtime.emptyUntil = time.Now().Add(-time.Second)
	runtime.claimMu.Unlock()
	_, _ = gate.Claim(context.Background(), "worker", []string{"job"}, time.Second)
	store.mu.Lock()
	claims = store.claims
	store.mu.Unlock()
	if claims != 2 {
		t.Fatalf("expired probe gate did not query store: %d", claims)
	}
}

func TestSchedulePollingTracksDefinitionCadence(t *testing.T) {
	for _, item := range []struct {
		interval time.Duration
		want     time.Duration
	}{{time.Millisecond, 100 * time.Millisecond}, {time.Second, time.Second}, {time.Minute, 5 * time.Second}} {
		if got := schedulePollInterval(item.interval); got != item.want {
			t.Fatalf("poll interval for %s = %s, want %s", item.interval, got, item.want)
		}
	}
}
