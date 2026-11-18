package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type BatchItemClaim struct {
	TaskID, ItemID uint
	Token          string
}
type BatchAttempt struct {
	Task BatchReceipt
	Item BatchItem
}
type BatchItemOutcome struct {
	Cause      error
	Message    string
	Panicked   bool
	RetryAfter time.Duration
}

type BatchItemRunResult struct {
	Failures []string
	Stop     bool
	Requeued bool
}

type BatchItemRepository interface {
	Begin(context.Context, BatchItemClaim) (BatchAttempt, error)
	Complete(context.Context, BatchItemClaim, int, BatchItemOutcome) error
}
type BatchBusinessExecutor interface {
	ExecuteBatchBusiness(context.Context, BatchReceipt, BatchItem) error
}
type BatchItemRunner struct {
	Repository BatchItemRepository
	Executor   BatchBusinessExecutor
}

func (s BatchItemRunner) Run(ctx context.Context, claim BatchItemClaim) (result BatchItemRunResult) {
	var attempt *BatchAttempt
	outcome := BatchItemOutcome{}
	defer func() {
		if recover() != nil {
			outcome = BatchItemOutcome{Cause: errors.New("executor panicked; verify external results before retry"), Panicked: true}
		}
		if attempt == nil {
			if outcome.Cause != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("item %d: %s", claim.ItemID, outcome.Cause.Error()))
			}
			return
		}
		if outcome.Cause != nil {
			outcome.Message = strings.TrimSpace(outcome.Cause.Error())
			if len(outcome.Message) > 2000 {
				outcome.Message = outcome.Message[:2000]
				for !utf8.ValidString(outcome.Message) && len(outcome.Message) > 0 {
					outcome.Message = outcome.Message[:len(outcome.Message)-1]
				}
			}
			var retryable Retryable
			if !outcome.Panicked && ctx.Err() == nil && attempt.Task.Attempts < attempt.Task.MaxAttempts && errors.As(outcome.Cause, &retryable) {
				outcome.RetryAfter = time.Duration(max(1, attempt.Task.Attempts)) * time.Second
			}
			if outcome.RetryAfter == 0 && !errors.Is(outcome.Cause, ErrBatchItemSkipped) {
				result.Failures = append(result.Failures, fmt.Sprintf("item %d: %s", claim.ItemID, outcome.Message))
			}
		}
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := s.Repository.Complete(finishCtx, claim, attempt.Item.Attempts, outcome); err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("item %d result not committed: %v", claim.ItemID, err))
		} else if outcome.RetryAfter > 0 {
			result.Requeued = true
		}
	}()
	if err := ctx.Err(); err != nil {
		return BatchItemRunResult{Failures: []string{err.Error()}}
	}
	if claim.TaskID == 0 || claim.ItemID == 0 || claim.Token == "" {
		return BatchItemRunResult{Failures: []string{ErrLeaseLost.Error()}}
	}
	current, err := s.Repository.Begin(ctx, claim)
	if err != nil {
		return BatchItemRunResult{Failures: []string{fmt.Sprintf("item %d: %v", claim.ItemID, err)}}
	}
	attempt = &current
	outcome.Cause = s.Executor.ExecuteBatchBusiness(ctx, current.Task, current.Item)
	return result
}
