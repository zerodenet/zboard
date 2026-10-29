package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
)

type diagnosticRetentionProbe struct {
	limit int
	bytes int64
	ready bool
	err   error
	ctx   context.Context
}

func (p *diagnosticRetentionProbe) HasPending(ctx context.Context) (bool, error) {
	p.ctx = ctx
	return p.ready, p.err
}
func (p *diagnosticRetentionProbe) Retire(ctx context.Context, limit int, budget int64) (metering.DiagnosticMaterialRetentionResult, error) {
	p.ctx, p.limit, p.bytes = ctx, limit, budget
	return metering.DiagnosticMaterialRetentionResult{Inspected: limit}, p.err
}
func TestDiagnosticMaterialJobKeepsBoundedLiveCapability(t *testing.T) {
	p := &diagnosticRetentionProbe{ready: true}
	s := &Services{DiagnosticMaterialRetention: metering.DiagnosticMaterialRetention{Repository: p}}
	j := s.DiagnosticMaterialRetentionJob()
	if j.ID != "traffic_diagnostic_retention" || j.Interval != time.Second || j.Timeout != time.Minute || !j.Immediate || !j.RetryFailures || !j.ReconcileAfterLoss || j.External {
		t.Fatal("retention job policy", j)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if ready, err := j.Ready(ctx); err != nil || !ready || p.ctx != ctx {
		t.Fatal("retention readiness", ready, err)
	}
	if err := j.Run(ctx); err != nil || p.limit != 32 || p.bytes != 256*1024 || p.ctx != ctx {
		t.Fatal("retention bounds/context", p, err)
	}
	failure := errors.New("retention transaction failed")
	next := &diagnosticRetentionProbe{err: failure}
	s.DiagnosticMaterialRetention.Repository = next
	if _, err := j.Ready(ctx); err != failure {
		t.Fatal("readiness failure hidden", err)
	}
	if err := j.Run(ctx); err != failure {
		t.Fatal("retirement failure hidden or frozen capability", err)
	}
}
