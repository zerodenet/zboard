package application

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

type PeriodicJob struct {
	ID                 string
	Name               string
	Interval           time.Duration
	Timeout            time.Duration
	External           bool
	Immediate          bool
	TimeZone           string
	MisfirePolicy      jobs.MisfirePolicy
	FirstPlannedAt     time.Time
	ReconcileAfterLoss bool
	// RetryFailures is restricted to reconcilers whose durable domain state
	// makes a repeated scan idempotent. Unknown outcomes are never retried.
	RetryFailures bool
	Ready         func(context.Context) (bool, error)
	Run           func(context.Context) error
}

// RegisterPeriodicJob keeps persisted scheduling policy in the application
// composition root. Transport adapters provide only readiness and execution
// ports; they do not construct durable job definitions.
func (s *Services) RegisterPeriodicJob(job PeriodicJob) error {
	if job.Run == nil {
		return jobs.ErrInvalid
	}
	group := ""
	if job.External {
		group = "external"
	}
	maxAttempts := 1
	retryBackoff := time.Duration(0)
	if job.RetryFailures {
		maxAttempts = 3
		retryBackoff = time.Second
	}
	return s.Jobs.RegisterWhen(jobs.Definition{
		ID: job.ID, Owner: "system", Name: job.Name, Handler: job.ID,
		ExecutionGroup: group, Resource: "core:" + job.ID, Revision: "1",
		Interval: job.Interval, Timeout: job.Timeout, Immediate: job.Immediate,
		TimeZone: job.TimeZone, MisfirePolicy: job.MisfirePolicy, FirstPlannedAt: job.FirstPlannedAt,
		MaxAttempts: maxAttempts, RetryBackoff: retryBackoff,
		ReconcileAfterLoss: job.ReconcileAfterLoss,
	}, func(ctx context.Context, _ jobs.Run) error {
		err := job.Run(ctx)
		if err != nil && job.RetryFailures {
			return jobs.Retry(err)
		}
		return err
	}, job.Ready)
}

func (s *Services) RemoveJob(id string) { s.Jobs.Remove(id) }
