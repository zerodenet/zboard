package handler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
)

type sharedDiagnosticRetentionProbe struct{ *sharedProjectionProbe }

func (p sharedDiagnosticRetentionProbe) HasPending(ctx context.Context) (bool, error) {
	return p.pending(ctx)
}
func (p sharedDiagnosticRetentionProbe) Retire(ctx context.Context, count int, budget int64) (metering.DiagnosticMaterialRetentionResult, error) {
	n, err := p.run(ctx, "traffic_diagnostic_retention", count, budget)
	return metering.DiagnosticMaterialRetentionResult{Inspected: n}, err
}

func TestSQLiteDiagnosticMaterialJobUsesSharedExecutor(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	prepareTestMaintenanceSource(t, h)
	assertDiagnosticMaterialJobExecutor(t, h)
}
func TestMySQLDiagnosticMaterialJobUsesSharedExecutor(t *testing.T) {
	assertDiagnosticMaterialJobExecutor(t, newMySQLBusinessOrderFixture(t).h)
}
func assertDiagnosticMaterialJobExecutor(t *testing.T, h *handlers) {
	t.Helper()
	entered := make(chan sharedProjectionCall, 2)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	probe := &sharedProjectionProbe{entered: entered, release: release, ready: true}
	h.services.DiagnosticMaterialRetention = metering.DiagnosticMaterialRetention{Repository: sharedDiagnosticRetentionProbe{probe}}
	h.StartTrafficDiagnosticRetentionWorker()
	h.StartTrafficDiagnosticRetentionWorker()
	select {
	case got := <-entered:
		if got.id != "traffic_diagnostic_retention" || got.count != 32 || got.bytes != 256*1024 {
			t.Fatal("unbounded retirement call", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("retention execution did not enter")
	}
	stop, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	err := h.services.Jobs.Drain(stop)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("in-flight retirement lost its local drain boundary", err)
	}
	once.Do(func() { close(release) })
	finish, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := h.services.Jobs.Drain(finish); err != nil {
		t.Fatal(err)
	}
	if probe.calls.Load() != 1 || probe.readyCalls.Load() == 0 {
		t.Fatal("retention duplicated or bypassed readiness")
	}
	rows, err := jobstore.New(h.db).Schedules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.ID != "traffic_diagnostic_retention" {
			continue
		}
		found = true
		if r.Name != "流量诊断材料保留" || r.Owner != "system" || r.Resource != "core:"+r.ID || r.Handler != r.ID || r.Timeout != time.Minute || r.Interval != time.Second || r.Runs != 1 || r.State != jobs.Succeeded {
			t.Fatal("durable retirement policy", r)
		}
	}
	if !found || h.jobReadiness("traffic_diagnostic_retention") == nil {
		t.Fatal("retention shared registration/readiness missing")
	}
}
