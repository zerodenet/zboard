package jobs

import (
	"context"
	"errors"
	"testing"
)

type runnerFixture struct {
	calls      int
	failures   []string
	finishLive bool
	cancel     context.CancelFunc
	panicNow   bool
	stop       bool
}

func (f *runnerFixture) Load(context.Context, uint, string) (LoadedBatch, error) {
	return LoadedBatch{Task: BatchReceipt{ID: 1}, Items: []BatchItem{{ID: 1}, {ID: 2}}}, nil
}

func (f *runnerFixture) Pending(context.Context) (bool, error)      { return true, nil }
func (f *runnerFixture) Ready(context.Context, int) ([]uint, error) { return nil, nil }
func (f *runnerFixture) Renew(context.Context, uint, string) error  { return nil }
func (f *runnerFixture) Finish(ctx context.Context, id uint, token string, failures []string) error {
	f.finishLive = ctx.Err() == nil
	f.failures = append([]string{}, failures...)
	return nil
}
func (f *runnerFixture) ExecuteBatchItem(ctx context.Context, task BatchReceipt, item BatchItem, token string) ([]string, bool) {
	f.calls++
	if f.panicNow {
		panic("private failure")
	}
	if f.cancel != nil {
		f.cancel()
	}
	if f.stop {
		return []string{"prerequisite failed"}, true
	}
	return nil, false
}
func TestBatchRunnerCancellationAndPanicStillFinish(t *testing.T) {
	for _, mode := range []string{"cancel", "panic", "prerequisite", "success"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fixture := &runnerFixture{panicNow: mode == "panic", stop: mode == "prerequisite"}
			if mode == "cancel" {
				fixture.cancel = cancel
			}
			runner := BatchRunner{Repository: fixture, Lifecycle: BatchLifecycle{Repository: fixture}, Executor: fixture}
			err := runner.Run(ctx, 1, "owner")
			if !fixture.finishLive {
				t.Fatal("finish inherited cancellation")
			}
			if mode == "success" {
				if err != nil || fixture.calls != 2 || len(fixture.failures) != 0 {
					t.Fatal(fixture, err)
				}
			} else if err == nil || fixture.calls != 1 || len(fixture.failures) == 0 {
				t.Fatal(fixture, err)
			}
		})
	}
	if err := (BatchRunner{}).Run(context.Background(), 0, ""); !errors.Is(err, ErrLeaseLost) {
		t.Fatal(err)
	}
}
