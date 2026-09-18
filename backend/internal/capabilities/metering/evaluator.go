package metering

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type EvaluationResult struct {
	SubscriptionID uint              `json:"subscription_id"`
	Evaluated      bool              `json:"evaluated"`
	Skipped        bool              `json:"skipped"`
	Reason         string            `json:"reason"`
	Policy         Policy            `json:"policy"`
	PolicySource   PolicySource      `json:"policy_source"`
	State          State             `json:"state"`
	Metrics        *TelemetryMetrics `json:"metrics,omitempty"`
}
type EvaluationSampler interface {
	Sample(context.Context, uint, int, int, time.Time) (TelemetryMetrics, error)
}
type EvaluationRepository interface {
	Apply(context.Context, uint, Policy, EvaluationObservation, time.Time) (EvaluationChange, error)
}

// Evaluator is the internal execution capability. User-facing triggers must
// authorize the actor before dispatching it through the task runtime.
type Evaluator struct {
	Source     EvaluationSource
	Sampler    EvaluationSampler
	Repository EvaluationRepository
}

func (s Evaluator) Evaluate(ctx context.Context, id uint, now time.Time) (EvaluationResult, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if err := ctx.Err(); err != nil {
			return EvaluationResult{}, err
		}
		out, err := s.evaluate(ctx, id, now)
		if !errors.Is(err, ErrEvaluationPolicyChanged) {
			return out, err
		}
	}
	return EvaluationResult{}, ErrEvaluationPolicyChanged
}
func (s Evaluator) evaluate(ctx context.Context, id uint, now time.Time) (EvaluationResult, error) {
	snapshot, err := s.Source.Snapshot(ctx, id)
	if err != nil {
		return EvaluationResult{}, err
	}
	policy := snapshot.Policy
	out := EvaluationResult{SubscriptionID: id, Policy: policy, PolicySource: snapshot.Source}
	if !policy.Enabled {
		out.Skipped = true
		out.Reason = "policy_disabled"
		return out, nil
	}
	if !EvaluationDue(snapshot.State.LastEvaluatedAt, policy.EvaluationIntervalSeconds, now) {
		out.State = snapshot.State
		out.Skipped = true
		out.Reason = "evaluation_interval_not_due"
		return out, nil
	}
	metrics, err := s.Sampler.Sample(ctx, id, policy.ConnectionStartWindowSeconds, policy.WorkingNodeWindowSeconds, now)
	if err != nil {
		return EvaluationResult{}, err
	}
	raw, err := json.Marshal(metrics)
	if err != nil {
		return EvaluationResult{}, err
	}
	change, err := s.Repository.Apply(ctx, id, policy, EvaluationObservation{CurrentActiveFlows: metrics.CurrentActiveFlows, ConnectionStarts: metrics.ReceivedConnectionStarts.Count, WorkingNodes: metrics.ReceivedWorkingNodes.Count, Completeness: metrics.TelemetryCompleteness, CoverageReason: metrics.Coverage.Reason, Metrics: raw}, now)
	if err != nil {
		return EvaluationResult{}, err
	}
	out.Metrics = &metrics
	out.State = change.State
	out.Evaluated = change.Evaluated
	out.Skipped = change.Skipped
	out.Reason = change.Reason
	return out, nil
}
