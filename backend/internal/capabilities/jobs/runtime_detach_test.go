package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRuntimeDetachCancelsAndWaitsForLocalHandler(t *testing.T) {
	store := &runtimeRemoteCancellationStore{observed: make(chan struct{}), finished: make(chan State, 1)}
	r := NewRuntime(store, "detaching", nil, nil)
	defer r.Close()
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	d := Definition{ID: "remote", Handler: "remote", Revision: "1", Timeout: 5 * time.Second}
	if err := r.Register(d, func(ctx context.Context, _ Run) error {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-release
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	waitRuntimeLifecycle(t, entered)
	detached := make(chan struct{})
	go func() { r.Detach(d.ID); close(detached) }()
	waitRuntimeLifecycle(t, canceled)
	select {
	case <-detached:
		t.Fatal("Detach returned before the handler finished")
	case <-time.After(25 * time.Millisecond):
	}
	if err := r.Register(d, func(context.Context, Run) error { return nil }); !errors.Is(err, ErrConflict) {
		t.Fatalf("replacement overtook local handler: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	waitRuntimeLifecycle(t, detached)
	if store.cancelCalls.Load() != 0 {
		t.Fatal("Detach invoked global revocation")
	}
	select {
	case state := <-store.finished:
		if state != Unknown {
			t.Fatalf("local interruption changed uncertain result rules: %s", state)
		}
	case <-time.After(time.Second):
		t.Fatal("local lease was not finalized")
	}
	if len(r.RegisteredJobs()) != 0 {
		t.Fatal("detached registration remained visible")
	}
	if err := r.Register(d, func(context.Context, Run) error { return nil }); err != nil {
		t.Fatal(err)
	}
	r.Detach(d.ID)
	r.Detach(d.ID)
	r.Detach("never-registered")
}

type detachSchedulingStore struct{ runtimeLifecycleStore }

func (s *detachSchedulingStore) Schedule(context.Context, Definition) error {
	s.scheduleOnce.Do(func() { close(s.scheduleEntered) })
	<-s.scheduleRelease
	return nil
}

func TestRuntimeDetachWaitsForScheduleOutsideLifecycleMutex(t *testing.T) {
	store := &detachSchedulingStore{runtimeLifecycleStore: runtimeLifecycleStore{scheduleEntered: make(chan struct{}), scheduleRelease: make(chan struct{})}}
	r := NewRuntime(store, "detaching-schedule", nil, nil)
	defer r.Close()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(store.scheduleRelease) }) }
	defer release()
	if err := r.Register(Definition{ID: "scheduled", Handler: "scheduled", Interval: time.Hour, Timeout: time.Second}, func(context.Context, Run) error { return nil }); err != nil {
		t.Fatal(err)
	}
	waitRuntimeLifecycle(t, store.scheduleEntered)
	detached := make(chan struct{})
	go func() { r.Detach("scheduled"); close(detached) }()
	select {
	case <-detached:
		t.Fatal("Detach returned before the schedule write finished")
	case <-time.After(25 * time.Millisecond):
	}
	assertRuntimeDrainBudget(t, r)
	release()
	waitRuntimeLifecycle(t, detached)
	if err := r.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	if store.cancelCalls.Load() != 0 {
		t.Fatal("Detach/Drain revoked durable work")
	}
}

func TestRuntimeDetachDoesNotCrossInFlightGlobalRevocation(t *testing.T) {
	store := &runtimeLifecycleStore{cancelEntered: make(chan struct{}), cancelRelease: make(chan struct{}), cancelErr: errors.New("global cancellation failed")}
	r := NewGatedRuntime(store, "detaching-during-global-stop", func() bool { return false }, nil, nil)
	defer r.Close()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(store.cancelRelease) }) }
	defer release()
	d := Definition{ID: "shared", Handler: "shared", Timeout: time.Second}
	if err := r.Register(d, func(context.Context, Run) error { return nil }); err != nil {
		t.Fatal(err)
	}
	deactivated := make(chan error, 1)
	go func() { deactivated <- r.Deactivate(t.Context(), d.ID) }()
	waitRuntimeLifecycle(t, store.cancelEntered)
	detached := make(chan struct{})
	go func() { r.Detach(d.ID); close(detached) }()
	select {
	case <-detached:
		t.Fatal("Detach crossed unfinished global cancellation")
	case <-time.After(25 * time.Millisecond):
	}
	if err := r.Register(d, func(context.Context, Run) error { return nil }); !errors.Is(err, ErrConflict) {
		t.Fatalf("Detach removed global registration fence: %v", err)
	}
	release()
	select {
	case err := <-deactivated:
		if !errors.Is(err, store.cancelErr) {
			t.Fatal("global error was hidden", err)
		}
	case <-time.After(time.Second):
		t.Fatal("global stop did not finish")
	}
	waitRuntimeLifecycle(t, detached)
	if store.cancelCalls.Load() != 1 {
		t.Fatalf("local detach made additional global cancellation calls: %d", store.cancelCalls.Load())
	}
	if err := r.Register(d, func(context.Context, Run) error { return nil }); err != nil {
		t.Fatal(err)
	}
}
