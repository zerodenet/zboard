package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

type materialRetentionProbe struct {
	limit int
	bytes int64
	ready bool
	err   error
	ctx   context.Context
}

func (p *materialRetentionProbe) HasPending(ctx context.Context) (bool, error) {
	p.ctx = ctx
	return p.ready, p.err
}
func (p *materialRetentionProbe) Retire(ctx context.Context, limit int, budget int64) (network.ObservationMaterialRetentionResult, error) {
	p.ctx, p.limit, p.bytes = ctx, limit, budget
	return network.ObservationMaterialRetentionResult{Inspected: limit}, p.err
}
func TestObservationMaterialJobKeepsBoundedLiveCapability(t *testing.T) {
	p := &materialRetentionProbe{ready: true}
	s := &Services{NodeObservationMaterialRetention: network.ObservationMaterialRetention{Repository: p}}
	j := s.NodeObservationMaterialRetentionJob()
	if j.ID != "node_observation_material_retention" || j.Interval != time.Second || j.Timeout != time.Minute || !j.Immediate || !j.RetryFailures || !j.ReconcileAfterLoss || j.External {
		t.Fatal("retention job policy", j)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if ready, err := j.Ready(ctx); err != nil || !ready || p.ctx != ctx {
		t.Fatal("retention readiness", ready, err)
	}
	if err := j.Run(ctx); !errors.Is(err, jobs.ErrContinue) || p.limit != 32 || p.bytes != 8*1024*1024 || p.ctx != ctx {
		t.Fatal("retention bounds/context", p, err)
	}
	failure := errors.New("retention transaction failed")
	next := &materialRetentionProbe{err: failure}
	s.NodeObservationMaterialRetention.Repository = next
	if _, err := j.Ready(ctx); err != failure {
		t.Fatal("readiness failure hidden", err)
	}
	if err := j.Run(ctx); err != failure {
		t.Fatal("retirement failure hidden or frozen capability", err)
	}
}
