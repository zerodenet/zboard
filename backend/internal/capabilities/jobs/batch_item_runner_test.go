package jobs

import (
	"context"
	"errors"
	"testing"
)

type itemRunnerFixture struct {
	beginPanic, executePanic bool
	businessErr              error
	task                     BatchReceipt
	completed                bool
	outcome                  BatchItemOutcome
}

func (f *itemRunnerFixture) Begin(context.Context, BatchItemClaim) (BatchAttempt, error) {
	if f.beginPanic {
		panic("begin failure")
	}
	return BatchAttempt{Task: f.task, Item: BatchItem{Attempts: 2}}, nil
}
func (f *itemRunnerFixture) Complete(ctx context.Context, c BatchItemClaim, attempt int, outcome BatchItemOutcome) error {
	f.completed = true
	f.outcome = outcome
	if attempt != 2 || ctx.Err() != nil {
		return errors.New("bad completion context")
	}
	return nil
}
func (f *itemRunnerFixture) ExecuteBatchBusiness(context.Context, BatchReceipt, BatchItem) error {
	if f.executePanic {
		panic("private message")
	}
	return f.businessErr
}
func TestBatchItemRunnerPanicDoesNotBecomeSuccess(t *testing.T) {
	for _, begin := range []bool{true, false} {
		f := &itemRunnerFixture{beginPanic: begin, executePanic: !begin}
		runner := BatchItemRunner{Repository: f, Executor: f}
		failures := runner.Run(context.Background(), BatchItemClaim{TaskID: 1, ItemID: 2, Token: "owner"}).Failures
		if len(failures) == 0 {
			t.Fatal("panic became success")
		}
		if begin && f.completed {
			t.Fatal("unstarted attempt completed")
		}
		if !begin && (!f.completed || !f.outcome.Panicked) {
			t.Fatal("panic evidence lost")
		}
	}
}

func TestBatchItemRetryRespectsAttemptBudget(t *testing.T) {
	busy := errors.New("temporary contention")
	for _, attempt := range []int{1, 3} {
		fixture := &itemRunnerFixture{businessErr: Retry(busy), task: BatchReceipt{Attempts: attempt, MaxAttempts: 3}}
		result := (BatchItemRunner{Repository: fixture, Executor: fixture}).Run(t.Context(), BatchItemClaim{TaskID: 1, ItemID: 2, Token: "owner"})
		if !fixture.completed || result.Requeued != (attempt < 3) || (len(result.Failures) > 0) != (attempt == 3) ||
			(fixture.outcome.RetryAfter > 0) != (attempt < 3) || !errors.Is(fixture.outcome.Cause, busy) {
			t.Fatalf("attempt=%d result=%+v outcome=%+v", attempt, result, fixture.outcome)
		}
	}
}
