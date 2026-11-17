package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type idleDrainStore struct{ emptyRuntimeStore }

func (*idleDrainStore) Schedule(context.Context, Definition) error        { return nil }
func (*idleDrainStore) Schedules(context.Context) ([]ScheduleView, error) { return nil, nil }
func (*idleDrainStore) CancelHandler(context.Context, string) error {
	panic("local drain revoked global work")
}

func TestRuntimeDrainWithoutRegistrationsIsConcurrentAndTerminal(t *testing.T) {
	store := &idleDrainStore{}
	r := NewRuntime(store, "idle", nil, nil)
	defer r.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			if err := r.Drain(ctx); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if err := r.Register(Definition{ID: "new", Handler: "new", Timeout: time.Second}, func(context.Context, Run) error { return nil }); !errors.Is(err, ErrRuntimeDraining) {
		t.Fatal("drained runtime accepted a registration", err)
	}
	if _, err := (runtimeExecutionStore{Store: store, runtime: r}).Claim(ctx, "idle", []string{"new"}, time.Second); !errors.Is(err, ErrEmpty) {
		t.Fatal("drained runtime reached durable admission", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.claims != 0 {
		t.Fatal("drain queried global queue", store.claims)
	}
}

func TestAlreadyCanceledDrainDoesNotStopAdmission(t *testing.T) {
	r := NewRuntime(&idleDrainStore{}, "idle", nil, nil)
	defer r.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := r.Drain(ctx); !errors.Is(err, context.Canceled) || r.draining.Load() {
		t.Fatal("canceled caller changed runtime admission", err)
	}
}
