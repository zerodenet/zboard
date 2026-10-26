package handler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

type sharedMaterialRetentionProbe struct{ *sharedProjectionProbe }

func (p sharedMaterialRetentionProbe) HasPending(ctx context.Context) (bool, error) {
	return p.pending(ctx)
}
func (p sharedMaterialRetentionProbe) Retire(ctx context.Context, count int, budget int64) (network.ObservationMaterialRetentionResult, error) {
	n, err := p.run(ctx, "node_observation_material_retention", count, budget)
	return network.ObservationMaterialRetentionResult{Inspected: n}, err
}

func TestSQLiteObservationMaterialJobUsesSharedExecutor(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	prepareTestMaintenanceSource(t, h)
	assertMaterialJobExecutor(t, h)
}
func TestMySQLObservationMaterialJobUsesSharedExecutor(t *testing.T) {
	assertMaterialJobExecutor(t, newMySQLBusinessOrderFixture(t).h)
}
func assertMaterialJobExecutor(t *testing.T, h *handlers) {
	t.Helper()
	entered := make(chan sharedProjectionCall, 2)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	probe := &sharedProjectionProbe{entered: entered, release: release, ready: true}
	h.services.NodeObservationMaterialRetention = network.ObservationMaterialRetention{Repository: sharedMaterialRetentionProbe{probe}}
	h.StartNodeObservationMaterialRetentionWorker()
	h.StartNodeObservationMaterialRetentionWorker()
	select {
	case got := <-entered:
		if got.id != "node_observation_material_retention" || got.count != 32 || got.bytes != 8*1024*1024 {
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
		if r.ID != "node_observation_material_retention" {
			continue
		}
		found = true
		if r.Name != "节点观察材料保留" || r.Owner != "system" || r.Resource != "core:"+r.ID || r.Handler != r.ID || r.Timeout != time.Minute || r.Interval != time.Second || r.Runs != 1 || r.State != jobs.Yielded || r.LastState != jobs.Yielded || r.Failures != 0 {
			t.Fatal("durable retirement policy", r)
		}
	}
	if !found || h.jobReadiness("node_observation_material_retention") == nil {
		t.Fatal("retention shared registration/readiness missing")
	}
}
