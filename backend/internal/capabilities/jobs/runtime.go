package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

type activeExecution struct {
	cancel      context.CancelFunc
	maintenance bool
}

type registration struct {
	maintenance bool
	definition  Definition
	handler     Handler
	ctx         context.Context
	cancel      context.CancelFunc
	active      sync.WaitGroup
	scheduling  sync.Mutex
	ready       func(context.Context) (bool, error)
	nextCheck   time.Time
}

// Runtime is the single local execution pool for all registered capabilities.
// Database admission remains authoritative across Runtime instances.
type Runtime struct {
	store   SchedulingStore
	worker  string
	paused  func() bool
	ready   func() bool
	report  func(error)
	mu      sync.Mutex
	entries map[string]*registration
	active  map[string]activeExecution
	ctx     context.Context
	cancel  context.CancelFunc
	done    sync.WaitGroup
	start   sync.Once
	wake    chan struct{}
	claimMu sync.Mutex
	// emptyUntil coalesces empty-queue probes from the fixed worker pool. A
	// successful claim clears it so backlog still fills all execution slots.
	emptyUntil time.Time
}

func NewRuntime(store SchedulingStore, worker string, paused func() bool, report func(error)) *Runtime {
	return NewGatedRuntime(store, worker, nil, paused, report)
}

// NewGatedRuntime separates bootstrap readiness from maintenance. Neither
// ordinary nor maintenance executions may bypass the startup gate.
func NewGatedRuntime(store SchedulingStore, worker string, ready, paused func() bool, report func(error)) *Runtime {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runtime{store: store, worker: worker, paused: paused, ready: ready, report: report, entries: map[string]*registration{}, active: map[string]activeExecution{}, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1)}

	return r
}

func (r *Runtime) Register(d Definition, h Handler) error {
	return r.RegisterWhen(d, h, nil)
}
func (r *Runtime) RegisterMaintenance(d Definition, h Handler) error {
	if d.Owner != "system" || d.Resource != MaintenanceResource || d.Interval != 0 {
		return ErrInvalid
	}
	return r.register(d, h, nil, true)
}
func (r *Runtime) RegisterWhen(d Definition, h Handler, ready func(context.Context) (bool, error)) error {
	if d.Resource == MaintenanceResource {
		return ErrInvalid
	}
	return r.register(d, h, ready, false)
}
func (r *Runtime) register(d Definition, h Handler, ready func(context.Context) (bool, error), maintenance bool) error {
	if d.ID == "" || d.Handler != d.ID || h == nil || d.Timeout <= 0 {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx.Err() != nil {
		return r.ctx.Err()
	}
	if old := r.entries[d.ID]; old != nil {
		return ErrConflict
	}
	ctx, cancel := context.WithCancel(r.ctx)
	r.entries[d.ID] = &registration{maintenance: maintenance, definition: d, handler: h, ctx: ctx, cancel: cancel, ready: ready}
	r.claimMu.Lock()
	r.emptyUntil = time.Time{}
	r.claimMu.Unlock()
	r.start.Do(func() { r.done.Add(1); go r.loop() })
	r.wakeScheduler()
	return nil
}

func (r *Runtime) Remove(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entry, err := r.deactivate(ctx, id)
	r.error(err)
	if entry != nil {
		entry.active.Wait()
	}
}
func (r *Runtime) Deactivate(ctx context.Context, id string) error {
	_, err := r.deactivate(ctx, id)
	return err
}

func (r *Runtime) deactivate(ctx context.Context, id string) (*registration, error) {
	r.mu.Lock()
	entry := r.entries[id]
	if entry != nil {
		entry.scheduling.Lock()
		delete(r.entries, id)
	}
	r.mu.Unlock()
	if entry == nil {
		return nil, r.store.CancelHandler(ctx, id)
	}
	defer entry.scheduling.Unlock()
	err := r.store.CancelHandler(ctx, id)
	entry.cancel()
	return entry, err
}

// CancelActive interrupts a locally executing lease after its durable state is
// changed to cancel_requested. Other instances observe the same state through
// the executor cancellation poll.
func (r *Runtime) CancelActive(id string) {
	r.mu.Lock()
	active, ok := r.active[id]
	r.mu.Unlock()
	if ok {
		active.cancel()
	}
}
func (r *Runtime) Schedules(ctx context.Context) ([]ScheduleView, error) {
	return r.store.Schedules(ctx)
}
func (r *Runtime) Close() { r.mu.Lock(); r.cancel(); r.mu.Unlock(); r.done.Wait() }

func (r *Runtime) loop() {
	defer r.done.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	// A fixed local pool is a delivery mechanism; persisted budget controls
	// admission. Never create an unbounded goroutine per queued intent.
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); r.work() }()
	}
	defer workers.Wait()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
		case <-r.wake:
		}
		if r.ready != nil && !r.ready() {
			continue
		}
		if r.paused != nil && r.paused() {
			r.mu.Lock()
			for _, active := range r.active {
				if !active.maintenance {
					active.cancel()
				}
			}
			r.mu.Unlock()
			continue
		}
		r.mu.Lock()
		entries := make([]*registration, 0, len(r.entries))
		for _, e := range r.entries {
			entries = append(entries, e)
		}
		r.mu.Unlock()
		for _, e := range entries {
			// Unregistration cancels first, then waits for any in-flight
			// schedule write before canceling the persisted pending intent.
			e.scheduling.Lock()
			now := time.Now()
			d := e.definition
			if e.ctx.Err() != nil || d.Interval <= 0 || (!e.nextCheck.IsZero() && now.Before(e.nextCheck)) {
				e.scheduling.Unlock()
				continue
			}
			e.nextCheck = now.Add(schedulePollInterval(d.Interval))
			ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
			if e.ready != nil {
				ready, err := e.ready(ctx)
				if err != nil {
					e.nextCheck = now.Add(time.Second)
					cancel()
					e.scheduling.Unlock()
					r.error(err)
					continue
				}
				d.SuppressDispatch = !ready
			}
			err := r.store.Schedule(ctx, d)
			cancel()
			if err != nil {
				e.nextCheck = now.Add(time.Second)
			}
			e.scheduling.Unlock()
			// A full queue leaves the due plan intact for the next check.
			if !errors.Is(err, ErrBackpressure) {
				r.error(err)
			}
		}
	}
}

func schedulePollInterval(interval time.Duration) time.Duration {
	if interval < 100*time.Millisecond {
		return 100 * time.Millisecond
	}
	if interval > 5*time.Second {
		return 5 * time.Second
	}
	return interval
}

func (r *Runtime) wakeScheduler() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *Runtime) reconcileSchedule(id string) {
	r.mu.Lock()
	entry := r.entries[id]
	r.mu.Unlock()
	if entry == nil || entry.definition.Interval <= 0 {
		return
	}
	entry.scheduling.Lock()
	entry.nextCheck = time.Time{}
	entry.scheduling.Unlock()
	r.wakeScheduler()
}
func (r *Runtime) work() {
	wake := time.NewTimer(0)
	defer wake.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-wake.C:
			wake.Reset(time.Second)
			if r.ready != nil && !r.ready() {
				continue
			}
			if r.paused != nil && r.paused() {
				r.mu.Lock()
				for _, active := range r.active {
					if !active.maintenance {
						active.cancel()
					}
				}
				r.mu.Unlock()
			}
			r.mu.Lock()
			handlers := map[string]Handler{}
			for id, entry := range r.entries {
				if r.paused != nil && r.paused() && !entry.maintenance {
					continue
				}
				e := entry
				handlers[id] = func(ctx context.Context, run Run) error {
					if r.ready != nil && !r.ready() {
						return context.Canceled
					}
					if r.paused != nil && r.paused() && !e.maintenance {
						return context.Canceled
					}
					if e.maintenance && (run.Owner != "system" || run.Resource != MaintenanceResource) {
						return ErrConflict
					}
					var input struct {
						Revision string `json:"revision"`
					}
					if json.Unmarshal([]byte(run.Payload), &input) != nil || input.Revision != e.definition.Revision {
						return ErrConflict
					}
					r.mu.Lock()
					if e.ctx.Err() != nil {
						r.mu.Unlock()
						return context.Canceled
					}
					e.active.Add(1)
					r.mu.Unlock()
					defer e.active.Done()
					call, cancel := context.WithTimeout(ctx, e.definition.Timeout)
					defer cancel()
					r.mu.Lock()
					r.active[run.ID] = activeExecution{cancel: cancel, maintenance: e.maintenance}
					r.mu.Unlock()
					defer func() { r.mu.Lock(); delete(r.active, run.ID); r.mu.Unlock() }()
					stop := context.AfterFunc(e.ctx, cancel)
					defer stop()
					return e.handler(call, run)
				}
			}
			r.mu.Unlock()
			if len(handlers) == 0 {
				continue
			}
			claim, err := (Executor{Store: runtimeExecutionStore{Store: r.store, runtime: r}, Worker: r.worker, Handlers: handlers, Timeout: time.Hour}).runOne(r.ctx)
			if claim.Run.Handler != "" {
				r.reconcileSchedule(claim.Run.Handler)
			}
			if err == nil {
				wake.Reset(0)
			} else if !errors.Is(err, ErrEmpty) {
				r.error(err)
			}
		}
	}
}

type runtimeExecutionStore struct {
	Store
	runtime *Runtime
}

func (s runtimeExecutionStore) Claim(ctx context.Context, worker string, handlers []string, timeout time.Duration) (Claim, error) {
	if s.runtime == nil {
		return Claim{}, ErrInvalid
	}
	s.runtime.claimMu.Lock()
	defer s.runtime.claimMu.Unlock()
	now := time.Now()
	if now.Before(s.runtime.emptyUntil) {
		return Claim{}, ErrEmpty
	}
	claim, err := s.Store.Claim(ctx, worker, handlers, timeout)
	if errors.Is(err, ErrEmpty) {
		s.runtime.emptyUntil = now.Add(time.Second)
	} else {
		s.runtime.emptyUntil = time.Time{}
	}
	return claim, err
}
func (r *Runtime) error(err error) {
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrCanceled) && r.report != nil {
		r.report(err)
	}
}
