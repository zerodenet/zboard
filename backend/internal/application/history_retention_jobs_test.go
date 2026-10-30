package application

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"testing"
	"time"
)

type historyPolicyProbe struct {
	pending bool
	failure observability.HistoryTarget
	last    observability.HistoryTarget
	limit   int
	cutoff  time.Time
}

func (p *historyPolicyProbe) ReconcileDefaults(context.Context) error { return nil }
func (p *historyPolicyProbe) RetentionDays(_ context.Context, _ string, fallback, _ int) (int, error) {
	return fallback, nil
}
func (p *historyPolicyProbe) HasPendingHistory(_ context.Context, target observability.HistoryTarget, before time.Time) (bool, error) {
	p.last, p.cutoff = target, before
	return p.pending, nil
}
func (p *historyPolicyProbe) Prune(_ context.Context, target observability.HistoryTarget, before time.Time, limit int) (int64, error) {
	p.last, p.cutoff, p.limit = target, before, limit
	if target == p.failure {
		return 0, errors.New("policy unavailable")
	}
	return 1, nil
}
func (p *historyPolicyProbe) PrunePeriodicRuns(_ context.Context, before time.Time, success bool) (int64, error) {
	p.cutoff = before
	p.last = observability.PeriodicFailure
	if success {
		p.last = observability.PeriodicSuccess
	}
	return 1, nil
}

type operationPolicyProbe struct {
	last  network.OperationKind
	limit int
}

func (p *operationPolicyProbe) PruneOperations(_ context.Context, kind network.OperationKind, _ time.Time, limit int) (int64, error) {
	p.last, p.limit = kind, limit
	return 1, nil
}
func (p *operationPolicyProbe) HasPendingOperations(_ context.Context, kind network.OperationKind, _ time.Time) (bool, error) {
	p.last = kind
	return false, nil
}
func TestHistoryRetentionJobsSeparateFailureReadinessAndBoundedExecution(t *testing.T) {
	probe := &historyPolicyProbe{failure: observability.AuditLogs}
	operations := &operationPolicyProbe{}
	service := &Services{HistoryRetention: observability.HistoryRetention{Repository: probe}, OperationHistory: network.OperationHistory{Store: operations}}
	defs := service.HistoryRetentionJobs()
	if len(defs) != 9 {
		t.Fatal("retention targets are not independent", len(defs))
	}
	seen := map[string]bool{}
	for _, job := range defs {
		if seen[job.ID] || job.Ready == nil || job.Run == nil || job.Timeout != 20*time.Second || job.External || !job.RetryFailures || !job.ReconcileAfterLoss {
			t.Fatal("unbounded or unsafe retention definition", job.ID)
		}
		seen[job.ID] = true
		if ready, err := job.Ready(t.Context()); err != nil || ready {
			t.Fatal("empty history produced an executable job", job.ID, ready, err)
		}
	}
	// A failing audit policy does not control the cursor or periodic-history job.
	if err := defs[0].Run(t.Context()); err == nil {
		t.Fatal("policy failure hidden")
	}
	for _, job := range defs[1:] {
		if err := job.Run(t.Context()); err != nil {
			t.Fatal("unrelated target blocked", job.ID, err)
		}
	}
	// Definitions dispatch the current capability after replacement/restart.
	replacement := &historyPolicyProbe{pending: true}
	service.HistoryRetention.Repository = replacement
	if ready, err := defs[4].Ready(t.Context()); err != nil || !ready || replacement.last != observability.ArchivedFlowCursors {
		t.Fatal("readiness froze old repository", ready, err)
	}
	if err := defs[4].Run(t.Context()); err != nil || replacement.limit != observability.HistoryBatchSize {
		t.Fatal("cursor batch bound missing", replacement.limit, err)
	}
	age := time.Since(replacement.cutoff)
	if age < 7*24*time.Hour-time.Minute || age > 7*24*time.Hour+time.Minute {
		t.Fatal("archive grace policy drift", age)
	}
	if operations.limit != network.HistoryBatchSize {
		t.Fatal("operation batch is unbounded", operations.limit)
	}
}
