package metering

import (
	"context"
	"errors"
	"testing"
	"time"
)

type changingEvaluation struct {
	policy         Policy
	samples, reads int
	always         bool
}

func (f *changingEvaluation) Snapshot(context.Context, uint) (EvaluationSnapshot, error) {
	f.reads++
	return EvaluationSnapshot{Policy: f.policy}, nil
}
func (*changingEvaluation) Candidates(context.Context, time.Time) ([]uint, error) { return nil, nil }
func (f *changingEvaluation) Sample(context.Context, uint, int, int, time.Time) (TelemetryMetrics, error) {
	f.samples++
	if f.samples == 1 || f.always {
		f.policy.Revision++
	}
	return TelemetryMetrics{}, nil
}
func (f *changingEvaluation) Apply(_ context.Context, _ uint, p Policy, _ EvaluationObservation, _ time.Time) (EvaluationChange, error) {
	if p.Revision != f.policy.Revision {
		return EvaluationChange{}, ErrEvaluationPolicyChanged
	}
	return EvaluationChange{Evaluated: true}, nil
}
func TestEvaluatorResamplesChangedPolicyWithBoundedRetry(t *testing.T) {
	f := &changingEvaluation{policy: DefaultPolicy("platform", 0)}
	f.policy.Enabled = true
	service := Evaluator{Source: f, Sampler: f, Repository: f}
	out, err := service.Evaluate(context.Background(), 1, time.Now())
	if err != nil || !out.Evaluated || f.samples != 2 || out.Policy.Revision != 1 {
		t.Fatalf("retry: %+v %v samples=%d", out, err, f.samples)
	}
	f.samples = 0
	f.always = true
	if _, err = service.Evaluate(context.Background(), 1, time.Now()); !errors.Is(err, ErrEvaluationPolicyChanged) || f.samples != 2 {
		t.Fatalf("unbounded retry: %d %v", f.samples, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reads := f.reads
	if _, err = service.Evaluate(ctx, 1, time.Now()); !errors.Is(err, context.Canceled) || f.reads != reads {
		t.Fatalf("canceled query: %v", err)
	}
}
