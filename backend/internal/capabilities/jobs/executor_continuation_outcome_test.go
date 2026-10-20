package jobs

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type continuationOutcomeStore struct {
	emptyRuntimeStore
	finished    State
	finishCount int
}

func (s *continuationOutcomeStore) Claim(context.Context, string, []string, time.Duration) (Claim, error) {
	return Claim{Run: Run{ID: "continuation-run", Submission: Submission{Handler: "continuation"}}, Token: "continuation-token", Worker: "worker", ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (s *continuationOutcomeStore) Finish(_ context.Context, _ Claim, state State) error {
	s.finished = state
	s.finishCount++
	return nil
}

func TestExecutorContinuationPreservesJoinedTerminalOutcome(t *testing.T) {
	for _, test := range []struct {
		name   string
		result error
		want   State
	}{
		{name: "continue", result: ErrContinue, want: Yielded},
		{name: "wrapped_continue", result: fmt.Errorf("more work: %w", ErrContinue), want: Yielded},
		{name: "joined_continue", result: errors.Join(ErrContinue, ErrContinue), want: Yielded},
		{name: "joined_failure", result: errors.Join(ErrContinue, errors.New("ordinary failure")), want: Failed},
		{name: "continue_uncertain", result: errors.Join(ErrContinue, ErrUncertain), want: Unknown},
		{name: "continue_deadline", result: errors.Join(ErrContinue, context.DeadlineExceeded), want: Unknown},
		{name: "continue_context_canceled", result: errors.Join(ErrContinue, context.Canceled), want: Unknown},
		{name: "continue_canceled", result: errors.Join(ErrContinue, ErrCanceled), want: Canceled},
		{name: "retry_continue", result: Retry(ErrContinue), want: RetryWait},
		{name: "retry_continue_uncertain", result: Retry(errors.Join(ErrContinue, ErrUncertain)), want: Unknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &continuationOutcomeStore{}
			executor := Executor{Store: store, Worker: "worker", Timeout: time.Minute, Handlers: map[string]Handler{"continuation": func(context.Context, Run) error { return test.result }}}
			err := executor.RunOne(t.Context())
			if store.finishCount != 1 || store.finished != test.want {
				t.Fatalf("terminal outcome lost: finished=%s count=%d want=%s err=%v", store.finished, store.finishCount, test.want, err)
			}
			if test.want == Yielded {
				if err != nil {
					t.Fatalf("pure continuation leaked as failure: %v", err)
				}
			} else if !errors.Is(err, test.result) {
				t.Fatalf("terminal cause lost: result=%v returned=%v", test.result, err)
			}
		})
	}
}
