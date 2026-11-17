package jobs

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type renewableExecutorStore struct {
	emptyRuntimeStore
	renewals      atomic.Int32
	fail          bool
	coverDeadline bool
	state         State
	deadline      time.Time
}

func (s *renewableExecutorStore) ClaimRenewable(context.Context, string, []string, time.Duration) (Claim, error) {
	s.deadline = time.Now().Add(5 * time.Second)
	return Claim{Run: Run{ID: "lease-run", Submission: Submission{Handler: "lease"}}, Token: "fenced-token", Worker: "worker", ExpiresAt: time.Now().Add(300 * time.Millisecond), DeadlineAt: s.deadline}, nil
}
func (s *renewableExecutorStore) RenewLease(context.Context, Claim) (time.Time, error) {
	s.renewals.Add(1)
	if s.fail {
		return time.Time{}, ErrLeaseLost
	}
	if s.coverDeadline {
		return s.deadline, nil
	}
	return minDeadline(time.Now().Add(60*time.Millisecond), s.deadline), nil
}

func TestExecutorStopsRenewingWhenLeaseAlreadyCoversHardDeadline(t *testing.T) {
	s := &renewableExecutorStore{coverDeadline: true}
	e := Executor{Store: s, Worker: "worker", Timeout: time.Minute, Handlers: map[string]Handler{"lease": func(ctx context.Context, _ Run) error {
		select {
		case <-time.After(400 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}}
	if err := e.RunOne(t.Context()); err != nil || s.state != Succeeded || s.renewals.Load() != 1 {
		t.Fatal(err, s.state, s.renewals.Load())
	}
}
func minDeadline(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func (s *renewableExecutorStore) Finish(_ context.Context, _ Claim, state State) error {
	s.state = state
	return nil
}
func TestExecutorRenewsLongExecutionAndStopsHeartbeatBeforeFinishing(t *testing.T) {
	s := &renewableExecutorStore{}
	e := Executor{Store: s, Worker: "worker", Timeout: time.Minute, Handlers: map[string]Handler{"lease": func(ctx context.Context, _ Run) error {
		select {
		case <-time.After(900 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}}
	if err := e.RunOne(t.Context()); err != nil || s.state != Succeeded || s.renewals.Load() < 2 {
		t.Fatal(err, s.state, s.renewals.Load())
	}
	count := s.renewals.Load()
	time.Sleep(100 * time.Millisecond)
	if s.renewals.Load() != count {
		t.Fatal("renewal leaked after completion")
	}
}
func TestExecutorLeaseLossCancelsHandlerAndKeepsUnknownOutcome(t *testing.T) {
	s := &renewableExecutorStore{fail: true}
	e := Executor{Store: s, Worker: "worker", Timeout: time.Minute, Handlers: map[string]Handler{"lease": func(ctx context.Context, _ Run) error { <-ctx.Done(); return ErrCanceled }}}
	if err := e.RunOne(t.Context()); !errors.Is(err, ErrLeaseLost) || s.state != Unknown {
		t.Fatal(err, s.state)
	}
}
