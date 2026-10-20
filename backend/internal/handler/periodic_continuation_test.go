package handler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func TestSQLitePeriodicContinuationPreservesExecutionOutcome(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	assertPeriodicContinuationPreservesExecutionOutcome(t, h)
}

func TestMySQLPeriodicContinuationPreservesExecutionOutcome(t *testing.T) {
	assertPeriodicContinuationPreservesExecutionOutcome(t, newMySQLOrderFixture(t).h)
}

func assertPeriodicContinuationPreservesExecutionOutcome(t *testing.T, h *handlers) {
	t.Helper()
	for _, test := range []struct {
		name     string
		result   error
		state    jobs.State
		failures uint64
	}{
		{name: "continue", result: jobs.ErrContinue, state: jobs.Yielded},
		{name: "wrapped_continue", result: fmt.Errorf("more backlog: %w", jobs.ErrContinue), state: jobs.Yielded},
		{name: "joined_continue", result: errors.Join(jobs.ErrContinue, jobs.ErrContinue), state: jobs.Yielded},
		{name: "failure", result: errors.New("retry-safe scan failure"), state: jobs.RetryWait},
		{name: "joined_failure", result: errors.Join(jobs.ErrContinue, errors.New("retry-safe scan failure")), state: jobs.RetryWait},
		{name: "uncertain", result: fmt.Errorf("scan result: %w", jobs.ErrUncertain), state: jobs.Unknown, failures: 1},
		{name: "continue_uncertain", result: errors.Join(jobs.ErrContinue, jobs.ErrUncertain), state: jobs.Unknown, failures: 1},
		{name: "continue_deadline", result: errors.Join(jobs.ErrContinue, context.DeadlineExceeded), state: jobs.Unknown, failures: 1},
		{name: "continue_context_canceled", result: errors.Join(jobs.ErrContinue, context.Canceled), state: jobs.Unknown, failures: 1},
		// Canceled without a persisted cancel_requested lease is fenced to
		// unknown by the real Store, even when the executor reports canceled.
		{name: "continue_canceled", result: errors.Join(jobs.ErrContinue, jobs.ErrCanceled), state: jobs.Unknown, failures: 1},
		{name: "retry_continue", result: jobs.Retry(jobs.ErrContinue), state: jobs.RetryWait},
	} {
		t.Run(test.name, func(t *testing.T) {
			// A separate composition root exercises the real application wrapper,
			// runtime, durable scheduling, executor and completion transaction.
			services := application.New(h.db, "0123456789abcdef0123456789abcdef")
			services.StartWork()
			defer services.Close()
			entered, release, canceled := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			var calls atomic.Int32
			id := "periodic_outcome_" + test.name
			err := services.RegisterPeriodicJob(application.PeriodicJob{
				ID: id, Name: id, Interval: time.Hour, Timeout: time.Minute,
				Immediate: true, RetryFailures: true,
				Run: func(ctx context.Context) error {
					if calls.Add(1) == 1 {
						close(entered)
					}
					select {
					case <-release:
						return test.result
					case <-ctx.Done():
						close(canceled)
						return ctx.Err()
					}
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("periodic handler did not enter")
			}
			budget, cancelBudget := context.WithTimeout(t.Context(), 25*time.Millisecond)
			err = services.Jobs.Drain(budget)
			cancelBudget()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("drain did not retain its deadline while handler was blocked: %v", err)
			}
			select {
			case <-canceled:
				t.Fatal("drain budget canceled the in-flight handler")
			default:
			}
			// This due intent is available to the registered handler, but a timed
			// out Drain must still stop further claims without canceling intent.
			pending, err := jobstore.New(h.db).Submit(t.Context(), jobs.Submission{
				Owner: "system", Key: id + ":after_drain", Handler: id,
				Resource: "core:" + id + ":after_drain", Payload: `{"revision":"1"}`,
			})
			if err != nil {
				t.Fatal(err)
			}
			releaseOnce.Do(func() { close(release) })
			finished, cancelFinished := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancelFinished()
			if err := services.Jobs.Drain(finished); err != nil {
				t.Fatal("drain did not wait for durable completion", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("draining runtime executed %d handlers", calls.Load())
			}
			var records []jobstore.Record
			if err := h.db.Where("handler = ? AND schedule_id <> ''", id).Find(&records).Error; err != nil || len(records) != 1 {
				t.Fatalf("expected exactly one periodic run: %+v %v", records, err)
			}
			run := records[0]
			var attempts []jobstore.Attempt
			if err := h.db.Where("run_id = ?", run.ID).Find(&attempts).Error; err != nil || len(attempts) != 1 {
				t.Fatalf("expected exactly one execution attempt: %+v %v", attempts, err)
			}
			attempt := attempts[0]
			var schedule jobstore.Schedule
			if err := h.db.First(&schedule, "id = ?", run.ScheduleID).Error; err != nil {
				t.Fatal(err)
			}
			want := string(test.state)
			if run.State != want || attempt.State != want || schedule.State != want || schedule.LastState != want {
				t.Fatalf("outcome lost: want=%s run=%s attempt=%s schedule=%s last=%s", want, run.State, attempt.State, schedule.State, schedule.LastState)
			}
			if run.AttemptCount != 1 || attempt.Number != 1 || run.MaxAttempts != 3 || run.RetryBackoffMS != 1000 ||
				schedule.Runs != 1 || schedule.Sequence != 1 || schedule.Failures != test.failures || schedule.RunID != run.ID ||
				attempt.FinishedAt == nil || schedule.LastStartedAt == nil || schedule.LastFinishedAt == nil {
				t.Fatalf("execution accounting lost: run=%+v attempt=%+v schedule=%+v", run, attempt, schedule)
			}
			if test.state == jobs.RetryWait {
				if run.FinishedAt != nil || run.Token != "" || run.Worker != "" || run.ExpiresAt != nil || !run.NotBefore.After(*attempt.FinishedAt) {
					t.Fatalf("deterministic failure did not retain retry backoff: %+v", run)
				}
			} else if run.FinishedAt == nil {
				t.Fatal("yielded or uncertain attempt lost completion time")
			}
			var unclaimed jobstore.Record
			if err := h.db.First(&unclaimed, "id = ?", pending.ID).Error; err != nil || unclaimed.State != string(jobs.Queued) || unclaimed.AttemptCount != 0 {
				t.Fatalf("drain changed pending durable intent: %+v %v", unclaimed, err)
			}
		})
	}
}
