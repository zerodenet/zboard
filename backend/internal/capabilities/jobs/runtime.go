package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var ErrRuntimeDraining = errors.New("job runtime is draining")

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

// RegisteredJob describes a handler attached to this process. It is independent
// of durable schedules and run history, which may be temporarily unavailable.
type RegisteredJob struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Owner           string `json:"owner"`
	IntervalSeconds int64  `json:"interval_seconds"`
	TimeoutSeconds  int64  `json:"timeout_seconds"`
}

// Runtime is the single local execution pool for all registered capabilities.
// Database admission remains authoritative across Runtime instances.
type Runtime struct {
	store       SchedulingStore
	worker      string
	paused      func() bool
	ready       func() bool
	report      func(error)
	mu          sync.Mutex
	entries     map[string]*registration
	active      map[string]activeExecution
	ctx         context.Context
	cancel      context.CancelFunc
	done        sync.WaitGroup
	start       sync.Once
	wake        chan struct{}
	workWake    chan struct{}
	claimMu     sync.Mutex
	draining    atomic.Bool
	drainOnce   sync.Once
	drainSignal chan struct{}
	drained     chan struct{}
	// emptyUntil coalesces empty-queue probes from the fixed worker pool. A
	// successful claim clears it so backlog still fills all execution slots.
	emptyUntil time.Time
	pressure   runtimePressure
}

func NewRuntime(store SchedulingStore, worker string, paused func() bool, report func(error)) *Runtime {
	return NewGatedRuntime(store, worker, nil, paused, report)
}

// NewGatedRuntime separates bootstrap readiness from maintenance. Neither
// ordinary nor maintenance executions may bypass the startup gate.
func NewGatedRuntime(store SchedulingStore, worker string, ready, paused func() bool, report func(error)) *Runtime {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runtime{store: store, worker: worker, paused: paused, ready: ready, report: report, entries: map[string]*registration{}, active: map[string]activeExecution{}, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), workWake: make(chan struct{}), drainSignal: make(chan struct{}), drained: make(chan struct{})}

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
	if r.ctx.Err() != nil {
		r.mu.Unlock()
		return r.ctx.Err()
	}
	if r.draining.Load() {
		r.mu.Unlock()
		return ErrRuntimeDraining
	}
	if old := r.entries[d.ID]; old != nil {
		r.mu.Unlock()
		return ErrConflict
	}
	ctx, cancel := context.WithCancel(r.ctx)
	r.entries[d.ID] = &registration{maintenance: maintenance, definition: d, handler: h, ctx: ctx, cancel: cancel, ready: ready}
	// The loop's first Add must remain under the same lifecycle lock as Drain
	// and Close. Never hold that lock while waiting for a durable Claim.
	r.start.Do(func() { r.done.Add(1); go r.loop() })
	r.mu.Unlock()
	r.claimMu.Lock()
	r.emptyUntil = time.Time{}
	r.claimMu.Unlock()
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

// Detach stops only this process's registration. Durable pending intent and
// foreign leases remain available to other runtimes; explicit permission or
// configuration revocation must still use Deactivate.
func (r *Runtime) Detach(id string) {
	r.mu.Lock()
	entry := r.entries[id]
	if entry != nil {
		// Handler admission checks and active.Add use the same mutex, so after
		// canceling here no new Add can race the initially empty active.Wait.
		entry.cancel()
	}
	r.mu.Unlock()
	if entry == nil {
		return
	}
	// Do not hold scheduling while waiting for a handler: the handler may be
	// finishing its own registered request. Canceled requests cannot submit.
	entry.active.Wait()
	entry.scheduling.Lock()
	r.mu.Lock()
	if r.entries[id] == entry {
		delete(r.entries, id)
	}
	r.mu.Unlock()
	entry.scheduling.Unlock()
}

func (r *Runtime) deactivate(ctx context.Context, id string) (*registration, error) {
	for {
		r.mu.Lock()
		entry := r.entries[id]
		r.mu.Unlock()
		if entry == nil {
			return nil, r.store.CancelHandler(ctx, id)
		}
		// Scheduling and registered requests may be waiting on the database.
		// Keep that wait outside mu so Drain can always enter its timed wait.
		entry.scheduling.Lock()
		r.mu.Lock()
		if r.entries[id] != entry {
			r.mu.Unlock()
			entry.scheduling.Unlock()
			continue
		}
		entry.cancel()
		r.mu.Unlock()
		// Keep this canceled entry as a same-name registration fence until the
		// global revocation finishes. Claims skip it, and scheduling/request
		// writes finish before CancelHandler through the scheduling mutex.
		err := r.store.CancelHandler(ctx, id)
		r.mu.Lock()
		delete(r.entries, id)
		r.mu.Unlock()
		entry.scheduling.Unlock()
		return entry, err
	}
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
func (r *Runtime) RegisteredJobs() []RegisteredJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RegisteredJob, 0, len(r.entries))
	for _, entry := range r.entries {
		d := entry.definition
		name := d.Name
		if name == "" {
			name = d.ID
		}
		out = append(out, RegisteredJob{ID: d.ID, Name: name, Owner: d.Owner, IntervalSeconds: int64(d.Interval.Seconds()), TimeoutSeconds: int64(d.Timeout.Seconds())})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Owner == out[j].Owner {
			return out[i].ID < out[j].ID
		}
		return out[i].Owner < out[j].Owner
	})
	return out
}
func (r *Runtime) Close() { r.mu.Lock(); r.cancel(); r.mu.Unlock(); r.done.Wait() }

// Drain stops local scheduling/admission and waits for already admitted work.
// A claim already entering the durable store is in flight and is awaited too.
// A caller timeout does not cancel handlers or alter durable intent; Close can
// explicitly interrupt local work using the existing uncertain-result rules.
// Once draining begins, this Runtime cannot accept another registration.
func (r *Runtime) Drain(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Register adds the runtime loop under mu. Reject new registrations before
	// waiting, so Wait cannot race a later Add on an initially empty runtime.
	r.mu.Lock()
	r.draining.Store(true)
	r.mu.Unlock()
	r.drainOnce.Do(func() {
		close(r.drainSignal)
		go func() { r.done.Wait(); close(r.drained) }()
	})
	select {
	case <-r.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

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
		if r.draining.Load() {
			return
		}
		select {
		case <-r.ctx.Done():
			return
		case <-r.drainSignal:
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
			if r.draining.Load() || e.ctx.Err() != nil || d.Interval <= 0 || (!e.nextCheck.IsZero() && now.Before(e.nextCheck)) {
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
			var dispatched ScheduleDispatch
			var err error
			if store, ok := r.store.(DispatchSchedulingStore); ok {
				dispatched, err = store.ScheduleDispatch(ctx, d)
			} else {
				err = r.store.Schedule(ctx, d)
			}
			cancel()
			if err != nil {
				e.nextCheck = now.Add(time.Second)
			} else if dispatched.NextAt.After(now) && dispatched.NextAt.Before(e.nextCheck) {
				e.nextCheck = dispatched.NextAt
			}
			e.scheduling.Unlock()
			if err == nil && dispatched.Runnable {
				r.wakeWorkers()
			}
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

func (r *Runtime) wakeWorkers() {
	if r.draining.Load() {
		return
	}
	// A committed ready intent invalidates an earlier empty probe. Keep the
	// established Claim -> mu lock order; no database or scheduling lock is held.
	r.claimMu.Lock()
	r.emptyUntil = time.Time{}
	r.claimMu.Unlock()
	r.mu.Lock()
	if r.workWake != nil {
		close(r.workWake)
	}
	r.workWake = make(chan struct{})
	r.mu.Unlock()
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
		if r.draining.Load() {
			return
		}
		r.mu.Lock()
		notification := r.workWake
		r.mu.Unlock()
		select {
		case <-r.ctx.Done():
			return
		case <-r.drainSignal:
			return
		case <-notification:
			wake.Reset(0)
		case <-wake.C:
			wake.Reset(time.Second)
			if r.ready != nil && !r.ready() {
				continue
			}
			// Maintenance readiness may read the database. Evaluate outside mu
			// and reuse this selection snapshot; execution checks it afresh below.
			paused := r.paused != nil && r.paused()
			if paused {
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
			registrations := map[string]*registration{}
			for id, entry := range r.entries {
				if entry.ctx.Err() != nil {
					continue
				}
				if paused && !entry.maintenance {
					continue
				}
				e := entry
				registrations[id] = e
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
			claim, err := (Executor{Store: runtimeExecutionStore{Store: r.store, runtime: r, registrations: registrations}, Worker: r.worker, Handlers: handlers, Timeout: time.Hour}).runOne(r.ctx)
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
	runtime       *Runtime
	registrations map[string]*registration
}

func (s runtimeExecutionStore) Claim(ctx context.Context, worker string, handlers []string, timeout time.Duration) (Claim, error) {
	if s.runtime == nil {
		return Claim{}, ErrInvalid
	}
	s.runtime.claimMu.Lock()
	defer s.runtime.claimMu.Unlock()
	if s.runtime.draining.Load() {
		return Claim{}, ErrEmpty
	}
	// A worker may have built its handler snapshot before deactivation and
	// waited behind another Claim. Recheck at admission, before creating a new
	// lease that would otherwise be classified unknown without calling a handler.
	s.runtime.mu.Lock()
	if s.runtime.entries != nil {
		live := make([]string, 0, len(handlers))
		for _, id := range handlers {
			if entry := s.runtime.entries[id]; entry != nil && entry.ctx.Err() == nil {
				if s.registrations != nil && s.registrations[id] != entry {
					continue
				}
				live = append(live, id)
			}
		}
		handlers = live
	}
	s.runtime.mu.Unlock()
	if len(handlers) == 0 {
		return Claim{}, ErrEmpty
	}
	now := time.Now()
	if now.Before(s.runtime.emptyUntil) {
		return Claim{}, ErrEmpty
	}
	claim, err := s.Store.Claim(ctx, worker, handlers, timeout)
	s.runtime.observeClaim(now, claim, err)
	if errors.Is(err, ErrEmpty) {
		s.runtime.emptyUntil = now.Add(time.Second)
	} else {
		s.runtime.emptyUntil = time.Time{}
	}
	return claim, err
}

func (s runtimeExecutionStore) CancellationRequested(ctx context.Context, claim Claim) (bool, error) {
	if observer, ok := s.Store.(CancellationObserver); ok {
		return observer.CancellationRequested(ctx, claim)
	}
	return false, nil
}
func (r *Runtime) error(err error) {
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrCanceled) && r.report != nil {
		r.report(err)
	}
}
