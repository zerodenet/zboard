package jobs

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type runtimeLifecycleStore struct {
	emptyRuntimeStore
	claimEntered    chan struct{}
	claimRelease    chan struct{}
	scheduleEntered chan struct{}
	scheduleRelease chan struct{}
	cancelEntered   chan struct{}
	cancelRelease   chan struct{}
	cancelErr       error
	claimOnce       sync.Once
	scheduleOnce    sync.Once
	cancelOnce      sync.Once
	claimCalls      atomic.Int32
	cancelCalls     atomic.Int32
}

func (s *runtimeLifecycleStore) Claim(ctx context.Context, _ string, _ []string, _ time.Duration) (Claim, error) {
	s.claimCalls.Add(1)
	if s.claimEntered != nil {
		s.claimOnce.Do(func() { close(s.claimEntered) })
	}
	if s.claimRelease != nil {
		select {
		case <-s.claimRelease:
		case <-ctx.Done():
			return Claim{}, ctx.Err()
		}
	}
	return Claim{}, ErrEmpty
}
func (s *runtimeLifecycleStore) Schedule(ctx context.Context, _ Definition) error {
	if s.scheduleEntered != nil {
		s.scheduleOnce.Do(func() { close(s.scheduleEntered) })
	}
	if s.scheduleRelease != nil {
		select {
		case <-s.scheduleRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (*runtimeLifecycleStore) Schedules(context.Context) ([]ScheduleView, error) { return nil, nil }
func (s *runtimeLifecycleStore) CancelHandler(ctx context.Context, _ string) error {
	s.cancelCalls.Add(1)
	if s.cancelEntered != nil {
		s.cancelOnce.Do(func() { close(s.cancelEntered) })
	}
	if s.cancelRelease != nil {
		select {
		case <-s.cancelRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.cancelErr
}

func waitRuntimeLifecycle(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("lifecycle gate was not reached")
	}
}

func assertRuntimeDrainBudget(t *testing.T, r *Runtime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Drain(ctx) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Drain while work blocked: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Drain could not honor context while lifecycle operation waited on storage")
	}
}

func TestRuntimeDrainBudgetWhileClaimBlocksConcurrentRegister(t *testing.T) {
	store := &runtimeLifecycleStore{claimEntered: make(chan struct{}), claimRelease: make(chan struct{})}
	r := NewRuntime(store, "blocked-claim", nil, nil)
	defer r.Close()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(store.claimRelease) }) }
	defer release()
	definition := Definition{ID: "first", Handler: "first", Timeout: time.Second}
	if err := r.Register(definition, func(context.Context, Run) error { return nil }); err != nil {
		t.Fatal(err)
	}
	waitRuntimeLifecycle(t, store.claimEntered)
	started, registered := make(chan struct{}), make(chan error, 1)
	go func() {
		close(started)
		registered <- r.Register(Definition{ID: "second", Handler: "second", Timeout: time.Second}, func(context.Context, Run) error { return nil })
	}()
	<-started
	// Let the registrar reach the already-held claim gate. All work behind that
	// gate stays blocked until the explicit release below.
	runtime.Gosched()
	assertRuntimeDrainBudget(t, r)
	release()
	select {
	case err := <-registered:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("registration did not finish after Claim release")
	}
	if err := r.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	if store.cancelCalls.Load() != 0 {
		t.Fatal("drain globally revoked work")
	}
}

func TestRuntimeDrainBudgetWhileScheduleBlocksDeactivate(t *testing.T) {
	store := &runtimeLifecycleStore{scheduleEntered: make(chan struct{}), scheduleRelease: make(chan struct{}), cancelErr: errors.New("global cancellation storage failure")}
	r := NewRuntime(store, "blocked-schedule", nil, nil)
	defer r.Close()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(store.scheduleRelease) }) }
	defer release()
	if err := r.Register(Definition{ID: "scheduled", Handler: "scheduled", Timeout: time.Second, Interval: time.Hour}, func(context.Context, Run) error { return nil }); err != nil {
		t.Fatal(err)
	}
	waitRuntimeLifecycle(t, store.scheduleEntered)
	started, deactivated := make(chan struct{}), make(chan error, 1)
	go func() { close(started); deactivated <- r.Deactivate(t.Context(), "scheduled") }()
	<-started
	runtime.Gosched()
	assertRuntimeDrainBudget(t, r)
	if store.cancelCalls.Load() != 0 {
		t.Fatal("global cancellation overtook an in-flight schedule write")
	}
	release()
	select {
	case err := <-deactivated:
		if !errors.Is(err, store.cancelErr) {
			t.Fatalf("Deactivate hid global storage failure: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Deactivate did not finish")
	}
	if err := r.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeDrainBudgetWhileMaintenanceReadBlocks(t *testing.T) {
	release := make(chan struct{})
	store := &runtimeLifecycleStore{claimEntered: make(chan struct{}), claimRelease: release}
	var r *Runtime
	blockedRead := make(chan struct{})
	var enteredOnce, releaseOnce sync.Once
	paused := func() bool {
		if r.mu.TryLock() {
			r.mu.Unlock()
			return false
		}
		// Simulate the real paused callback's database/cache wait. In the old
		// path this occurs inside the worker's own mu critical section.
		enteredOnce.Do(func() { close(blockedRead) })
		<-release
		return false
	}
	r = NewRuntime(store, "blocked-maintenance-read", paused, nil)
	defer r.Close()
	defer releaseOnce.Do(func() { close(release) })
	if err := r.Register(Definition{ID: "maintenance-read", Handler: "maintenance-read", Timeout: time.Second}, func(context.Context, Run) error { return nil }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blockedRead:
	case <-store.claimEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("worker reached neither maintenance read nor durable claim")
	}
	assertRuntimeDrainBudget(t, r)
	releaseOnce.Do(func() { close(release) })
	if err := r.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeDeactivateFencesCachedClaimsUntilGlobalCancellationCompletes(t *testing.T) {
	store := &runtimeLifecycleStore{cancelEntered: make(chan struct{}), cancelRelease: make(chan struct{}), cancelErr: errors.New("global cancellation storage failure")}
	r := NewGatedRuntime(store, "deactivating", func() bool { return false }, nil, nil)
	defer r.Close()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(store.cancelRelease) }) }
	defer release()
	d := Definition{ID: "stopping", Handler: "stopping", Timeout: time.Second}
	if err := r.Register(d, func(context.Context, Run) error { return nil }); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	cached := runtimeExecutionStore{Store: store, runtime: r, registrations: map[string]*registration{d.ID: r.entries[d.ID]}}
	r.mu.Unlock()
	deactivated := make(chan error, 1)
	go func() { deactivated <- r.Deactivate(t.Context(), d.ID) }()
	waitRuntimeLifecycle(t, store.cancelEntered)
	if err := r.Register(d, func(context.Context, Run) error { return nil }); !errors.Is(err, ErrConflict) {
		t.Fatalf("same-name replacement overtook global cancellation: %v", err)
	}
	var executions atomic.Int32
	executor := Executor{Store: cached, Worker: "cached", Timeout: time.Second, Handlers: map[string]Handler{d.ID: func(context.Context, Run) error { executions.Add(1); return nil }}}
	err := executor.RunOne(t.Context())
	if !errors.Is(err, ErrEmpty) || store.claimCalls.Load() != 0 || executions.Load() != 0 {
		t.Fatalf("deactivated cached handler reached admission: err=%v claims=%d executions=%d", err, store.claimCalls.Load(), executions.Load())
	}
	release()
	select {
	case err := <-deactivated:
		if !errors.Is(err, store.cancelErr) {
			t.Fatalf("Deactivate hid failed global cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Deactivate did not finish")
	}
	if err := r.Register(d, func(context.Context, Run) error { return nil }); err != nil {
		t.Fatalf("completed local stop retained registration: %v", err)
	}
	err = executor.RunOne(t.Context())
	if !errors.Is(err, ErrEmpty) || store.claimCalls.Load() != 0 || executions.Load() != 0 {
		t.Fatalf("old handler snapshot admitted a replacement generation: err=%v claims=%d executions=%d", err, store.claimCalls.Load(), executions.Load())
	}
}

type runtimeRemoteCancellationStore struct {
	runtimeLifecycleStore
	claimed         atomic.Bool
	remoteRequested atomic.Bool
	observed        chan struct{}
	observeOnce     sync.Once
	finished        chan State
}

func (s *runtimeRemoteCancellationStore) Claim(context.Context, string, []string, time.Duration) (Claim, error) {
	if s.claimed.Swap(true) {
		return Claim{}, ErrEmpty
	}
	return Claim{Run: Run{ID: "remote-run", Submission: Submission{Handler: "remote", Payload: `{"revision":"1"}`}}, Token: "token", Worker: "remote-worker", ExpiresAt: time.Now().Add(5 * time.Second)}, nil
}
func (s *runtimeRemoteCancellationStore) CancellationRequested(context.Context, Claim) (bool, error) {
	requested := s.remoteRequested.Load()
	if requested {
		s.observeOnce.Do(func() { close(s.observed) })
	}
	return requested, nil
}
func (s *runtimeRemoteCancellationStore) Finish(_ context.Context, _ Claim, state State) error {
	s.finished <- state
	return nil
}

func TestRuntimeExecutorPollsRemoteCancellationThroughClaimGate(t *testing.T) {
	store := &runtimeRemoteCancellationStore{observed: make(chan struct{}), finished: make(chan State, 1)}
	r := NewRuntime(store, "remote-worker", nil, nil)
	defer r.Close()
	entered := make(chan struct{})
	if err := r.Register(Definition{ID: "remote", Handler: "remote", Revision: "1", Timeout: 5 * time.Second}, func(ctx context.Context, _ Run) error { close(entered); <-ctx.Done(); return ctx.Err() }); err != nil {
		t.Fatal(err)
	}
	waitRuntimeLifecycle(t, entered)
	store.remoteRequested.Store(true)
	select {
	case <-store.observed:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("runtime wrapper hid the durable cancellation observer")
	}
	select {
	case state := <-store.finished:
		if state != Unknown {
			t.Fatalf("cancellation changed uncertain-result classification: %s", state)
		}
	case <-time.After(time.Second):
		t.Fatal("remote cancellation did not interrupt handler and finish its lease")
	}
}
