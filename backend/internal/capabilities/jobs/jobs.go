// Package jobs owns execution intent and worker contracts. It does not depend on
// HTTP, plugin protocols, ORM models or a particular persistence driver.
package jobs

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalid      = errors.New("invalid job request")
	ErrConflict     = errors.New("job request conflicts with existing intent")
	ErrEmpty        = errors.New("no eligible job or execution slot")
	ErrLeaseLost    = errors.New("job execution lease lost")
	ErrUncertain    = errors.New("job result requires verification")
	ErrCanceled     = errors.New("job execution safely canceled")
	ErrContinue     = errors.New("job yielded with more work available")
	ErrBackpressure = errors.New("job queue admission limit reached")
)

// Retryable marks a deterministic failure that the owning domain has declared
// safe to execute again. Timeouts, cancellation, panics and uncertain results
// deliberately bypass this path because their external side effects are not
// known.
type Retryable struct{ Cause error }

func (e Retryable) Error() string {
	if e.Cause == nil {
		return "job execution may be retried"
	}
	return e.Cause.Error()
}

func (e Retryable) Unwrap() error { return e.Cause }

func Retry(err error) error {
	if err == nil {
		err = errors.New("job execution may be retried")
	}
	return Retryable{Cause: err}
}

// MaintenanceResource reserves the same global execution pool for host maintenance.
const MaintenanceResource = "core:maintenance"

// Pending limits are host policy shared by every producer and worker. Plugin
// admissions leave room for core work; running and historical runs do not count.
const (
	MaxPending            = 4096
	MaxPluginPending      = 1024
	MaxPluginOwnerPending = 128
)

type State string

const (
	Queued    State = "queued"
	RetryWait State = "retry_wait"
	Running   State = "running"
	// CancelRequested retains the active lease while asking its worker to stop.
	// It is not a terminal claim about remote side effects.
	CancelRequested State = "cancel_requested"
	Succeeded       State = "succeeded"
	Failed          State = "failed"
	Unknown         State = "unknown"
	Yielded         State = "yielded"
	Interrupted     State = "interrupted"
	Canceled        State = "canceled"
)

// Owner is supplied by the authenticated adapter, never copied from a public
// request body. Key is scoped to that owner; Resource serializes related work.
type Submission struct {
	Timeout        time.Duration `json:",omitempty"`
	MaxAttempts    int           `json:",omitempty"`
	RetryBackoff   time.Duration `json:",omitempty"`
	Owner          string
	Key            string
	Handler        string
	Resource       string
	ExecutionGroup string
	Payload        string
	NotBefore      time.Time
	// DispatchLane is a trusted scheduling category. Empty values are derived
	// from owner/execution group by the durable store.
	DispatchLane string
}

type Run struct {
	ID string
	Submission
	State      State
	Attempt    int
	PlannedAt  *time.Time
	CreatedAt  time.Time
	FinishedAt *time.Time
}

// Token fences completion. An expired execution becomes unknown rather than
// automatically replaying a potentially non-idempotent external operation.
type Claim struct {
	Run       Run
	Token     string
	Worker    string
	ExpiresAt time.Time
}

type Store interface {
	Submit(context.Context, Submission) (Run, error)
	Claim(context.Context, string, []string, time.Duration) (Claim, error)
	Finish(context.Context, Claim, State) error
	List(context.Context, string, int, int) ([]Run, error)
}

// CancellationObserver is optional so small in-memory Store implementations do
// not need cancellation machinery. Durable stores implement it to propagate a
// request across application instances.
type CancellationObserver interface {
	CancellationRequested(context.Context, Claim) (bool, error)
}

type Handler func(context.Context, Run) error

// Executor processes one durable intent. Core and plugin adapters register in
// the same map and claim against the same store-wide budget.
type Executor struct {
	Store    Store
	Worker   string
	Handlers map[string]Handler
	Timeout  time.Duration
}

func (e Executor) RunOne(ctx context.Context) error {
	_, err := e.runOne(ctx)
	return err
}

// runOne additionally returns the claimed execution so Runtime can promptly
// reconcile only the schedule that just finished. Public callers retain the
// smaller error-only contract through RunOne.
func (e Executor) runOne(ctx context.Context) (claimed Claim, err error) {
	if e.Store == nil || e.Worker == "" || e.Timeout <= 0 || len(e.Handlers) == 0 {
		return claimed, ErrInvalid
	}
	names := make([]string, 0, len(e.Handlers))
	for name, h := range e.Handlers {
		if h == nil || name == "" {
			return claimed, ErrInvalid
		}
		names = append(names, name)
	}
	c, err := e.Store.Claim(ctx, e.Worker, names, e.Timeout)
	if err != nil {
		return claimed, err
	}
	claimed = c
	runCtx, cancel := context.WithDeadline(ctx, c.ExpiresAt)
	defer cancel()
	pollCtx, stopPolling := context.WithCancel(runCtx)
	defer stopPolling()
	if observer, ok := e.Store.(CancellationObserver); ok {
		go func() {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-pollCtx.Done():
					return
				case <-ticker.C:
					requested, checkErr := observer.CancellationRequested(pollCtx, c)
					if checkErr == nil && requested {
						cancel()
						return
					}
				}
			}
		}()
	}
	state := Failed
	defer func() {
		if recover() != nil {
			state = Unknown
			err = errors.New("job handler panicked; result requires verification")
		}
		if errors.Is(err, ErrCanceled) {
			state = Canceled
		} else if runCtx.Err() != nil || errors.Is(err, ErrUncertain) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			state = Unknown
		}
		// Completion must still be recorded when the caller has canceled.
		finishCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		err = errors.Join(err, e.Store.Finish(finishCtx, c, state))
	}()
	err = e.Handlers[c.Run.Handler](runCtx, c.Run)
	var retryable Retryable
	if errors.As(err, &retryable) {
		state = RetryWait
	} else if errors.Is(err, ErrContinue) {
		state = Yielded
		err = nil
	} else if err == nil {
		state = Succeeded
	}
	return claimed, err
}
