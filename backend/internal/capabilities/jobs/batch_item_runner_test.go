package jobs

import (
	"context"
	"errors"
	"testing"
)

type itemRunnerFixture struct {
	beginPanic, executePanic bool
	completed                bool
	outcome                  BatchItemOutcome
}

func (f *itemRunnerFixture) Begin(context.Context, BatchItemClaim) (BatchAttempt, error) {
	if f.beginPanic {
		panic("begin failure")
	}
	return BatchAttempt{Item: BatchItem{Attempts: 2}}, nil
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
	return nil
}
func TestBatchItemRunnerPanicDoesNotBecomeSuccess(t *testing.T) {
	for _, begin := range []bool{true, false} {
		f := &itemRunnerFixture{beginPanic: begin, executePanic: !begin}
		runner := BatchItemRunner{Repository: f, Executor: f}
		failures := runner.Run(context.Background(), BatchItemClaim{TaskID: 1, ItemID: 2, Token: "owner"})
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
