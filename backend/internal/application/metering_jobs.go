package application

import (
	"context"
	"encoding/json"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"time"
)

func (s *Services) RegisterFairUseJobs() error {
	return s.Jobs.Register(jobs.Definition{ID: metering.EvaluationJob, Handler: metering.EvaluationJob, Owner: "system", Name: "订阅公平使用评估", Resource: "core:fair_use", Revision: "1", Timeout: 5 * time.Minute}, s.ExecuteFairUseJob)
}
func (s *Services) ExecuteFairUseJob(ctx context.Context, run jobs.Run) error {
	var input metering.EvaluationRequest
	if run.Owner != "system" || run.Handler != metering.EvaluationJob || run.Resource != "core:fair_use" || json.Unmarshal([]byte(run.Payload), &input) != nil || input.Revision != "1" || input.Actor == 0 || input.SubscriptionID == 0 {
		return jobs.ErrInvalid
	}
	if err := (meteringstore.EvaluationRequests{DB: s.Identity.db}).Authorize(ctx, input.Actor, input.SubscriptionID); err != nil {
		return err
	}
	evaluator := s.FairUseEvaluator
	evaluator.Repository = meteringstore.EvaluationWriter{DB: s.Identity.db, Actor: input.Actor}
	_, err := evaluator.Evaluate(ctx, input.SubscriptionID, time.Now().UTC())
	return err
}
