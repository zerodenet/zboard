package jobs

import (
	"context"
	"errors"
	"strings"
	"time"
)

type LoadedBatch struct {
	Task  BatchReceipt
	Items []BatchItem
}
type BatchRunRepository interface {
	Load(context.Context, uint, string) (LoadedBatch, error)
}
type BatchItemExecutor interface {
	// The business adapter may stop dependent targets after a failed prerequisite.
	ExecuteBatchItem(context.Context, BatchReceipt, BatchItem, string) ([]string, bool)
}
type BatchRunner struct {
	Repository BatchRunRepository
	Lifecycle  BatchLifecycle
	Executor   BatchItemExecutor
}

// Run consumes one already-admitted slot and executes its targets sequentially.
// It never creates a second queue or independently admits work.
func (s BatchRunner) Run(ctx context.Context, id uint, token string) (result error) {
	if id == 0 || token == "" {
		return ErrLeaseLost
	}
	batch, err := s.Repository.Load(ctx, id, token)
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	heartbeat := make(chan error, 1)
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if err := s.Lifecycle.Renew(runCtx, id, token); err != nil {
					heartbeat <- err
					cancel()
					return
				}
			}
		}
	}()
	failures := []string{}
	defer func() {
		if recover() != nil {
			failures = append(failures, "task executor panicked; review operation results before retry")
		}
		cancel()
		<-done
		select {
		case err := <-heartbeat:
			failures = append(failures, err.Error())
		default:
		}
		// Cancellation stops execution, but a bounded independent write records its
		// result; lease fencing still prevents stale owners from committing.
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer finishCancel()
		result = s.Lifecycle.Finish(finishCtx, id, token, failures)
		if len(failures) > 0 {
			result = errors.Join(result, errors.New(strings.Join(failures, "; ")))
		}
	}()
	for _, item := range batch.Items {
		if err := runCtx.Err(); err != nil {
			failures = append(failures, err.Error())
			break
		}
		itemFailures, stop := s.Executor.ExecuteBatchItem(runCtx, batch.Task, item, token)
		failures = append(failures, itemFailures...)
		if stop {
			break
		}
	}
	return nil
}
