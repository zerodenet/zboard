package metering

import (
	"context"
	"errors"
	"testing"
	"time"
)

type retentionProbe struct {
	calls            []string
	cutoffs          []time.Time
	limits           []int
	rawErr, eventErr error
}

func (p *retentionProbe) PruneFlowStarts(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	p.calls = append(p.calls, "raw")
	p.cutoffs = append(p.cutoffs, cutoff)
	p.limits = append(p.limits, limit)
	if p.rawErr != nil {
		return 0, p.rawErr
	}
	return int64(limit), nil
}
func (p *retentionProbe) PruneEvaluationEvents(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	p.calls = append(p.calls, "events")
	p.cutoffs = append(p.cutoffs, cutoff)
	p.limits = append(p.limits, limit)
	if p.eventErr != nil {
		return 0, p.eventErr
	}
	return int64(limit), nil
}

func TestObservationRetentionRunsOneBoundedBatchPerTable(t *testing.T) {
	p := &retentionProbe{}
	now := time.Date(2026, 10, 20, 8, 0, 0, 0, time.FixedZone("fixture", 8*3600))
	result, err := (ObservationRetention{Repository: p}).Run(context.Background(), now)
	if err != nil || result.FlowStarts != 1000 || result.EvaluationEvents != 1000 || len(p.calls) != 2 {
		t.Fatalf("not exactly one bounded batch per table: %+v %v calls=%v", result, err, p.calls)
	}
	for i := range p.calls {
		if !p.cutoffs[i].Equal(now.UTC().Add(-15*24*time.Hour)) || p.limits[i] != ObservationRetentionBatchLimit {
			t.Fatalf("horizon or batch changed: %v %v", p.cutoffs, p.limits)
		}
	}
}

func TestObservationRetentionJoinsFailuresWithoutSkippingIndependentTable(t *testing.T) {
	rawErr, eventErr := errors.New("raw failed"), errors.New("events failed")
	for _, both := range []bool{false, true} {
		p := &retentionProbe{rawErr: rawErr}
		if both {
			p.eventErr = eventErr
		}
		result, err := (ObservationRetention{Repository: p}).Run(context.Background(), time.Now())
		if !errors.Is(err, rawErr) || len(p.calls) != 2 || result.FlowStarts != 0 {
			t.Fatalf("first failure escaped or second table skipped: %+v %v %v", result, err, p.calls)
		}
		if both && !errors.Is(err, eventErr) || !both && result.EvaluationEvents != 1000 {
			t.Fatalf("second outcome lost: %+v %v", result, err)
		}
	}
}
