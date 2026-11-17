package handler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func TestPeriodicObservationPreservesContinuationAndFailureMeaning(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	for _, test := range []struct {
		name   string
		result error
		failed bool
	}{
		{name: "success"},
		{name: "continue", result: jobs.ErrContinue},
		{name: "wrapped_continue", result: fmt.Errorf("remaining backlog: %w", jobs.ErrContinue)},
		{name: "joined_continue", result: errors.Join(jobs.ErrContinue, jobs.ErrContinue)},
		{name: "failure", result: errors.New("projection unavailable"), failed: true},
		{name: "joined_failure", result: errors.Join(jobs.ErrContinue, errors.New("projection unavailable")), failed: true},
		{name: "uncertain", result: fmt.Errorf("projection outcome: %w", jobs.ErrUncertain), failed: true},
		{name: "continue_uncertain", result: errors.Join(jobs.ErrContinue, jobs.ErrUncertain), failed: true},
		{name: "continue_deadline", result: errors.Join(jobs.ErrContinue, context.DeadlineExceeded), failed: true},
		{name: "continue_context_canceled", result: errors.Join(jobs.ErrContinue, context.Canceled), failed: true},
		{name: "continue_canceled", result: errors.Join(jobs.ErrContinue, jobs.ErrCanceled), failed: true},
		{name: "retry_continue", result: jobs.Retry(jobs.ErrContinue), failed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			id := "periodic_observation_" + test.name
			finish := h.observeJob(id, "periodic", time.Second, 1)
			finish(test.result)
			for _, row := range h.backgroundJobs().observations.Snapshot() {
				if row.ID != id {
					continue
				}
				if row.Runs != 1 || row.Running != 0 || row.State != "idle" || row.LastStartedAt == nil || row.LastFinishedAt == nil {
					t.Fatalf("bounded execution observation lost: %+v", row)
				}
				if test.failed {
					if row.Failures != 1 || row.LastResult != "failed" || row.LastError != test.result.Error() {
						t.Fatalf("real failure or uncertain outcome hidden: %+v", row)
					}
				} else if row.Failures != 0 || row.LastResult != "succeeded" || row.LastError != "" {
					// succeeded describes this bounded execution. Durable jobs retain
					// yielded and pending readiness retains any remaining backlog.
					t.Fatalf("successful continuation counted as failure: %+v", row)
				}
				return
			}
			t.Fatal("periodic execution observation missing")
		})
	}
}
