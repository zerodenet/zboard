package handler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

type sharedProjectionCall struct {
	id    string
	count int
	bytes int64
}
type sharedProjectionProbe struct {
	entered           chan sharedProjectionCall
	release           <-chan struct{}
	calls, readyCalls atomic.Int32
	ready             bool
	readyErr, runErr  error
}

func (p *sharedProjectionProbe) pending(context.Context) (bool, error) {
	p.readyCalls.Add(1)
	return p.ready, p.readyErr
}
func (p *sharedProjectionProbe) run(ctx context.Context, id string, count int, bytes int64) (int, error) {
	p.calls.Add(1)
	select {
	case p.entered <- sharedProjectionCall{id, count, bytes}:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	select {
	case <-p.release:
		return count, p.runErr
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

type sharedUsageProbe struct{ *sharedProjectionProbe }

func (p sharedUsageProbe) HasPending(ctx context.Context) (bool, error) { return p.pending(ctx) }
func (p sharedUsageProbe) Drain(ctx context.Context, count int) (int, error) {
	return p.run(ctx, "usage_projection", count, 0)
}

type sharedObservationProbe struct{ *sharedProjectionProbe }

func (p sharedObservationProbe) HasPending(ctx context.Context) (bool, error) { return p.pending(ctx) }
func (p sharedObservationProbe) Drain(ctx context.Context, count int, bytes int64) (int, error) {
	return p.run(ctx, "node_observation", count, bytes)
}

func TestSQLiteDefaultProjectionJobsUseSharedComposition(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	prepareTestMaintenanceSource(t, h)
	assertDefaultProjectionJobsUseSharedComposition(t, h)
}
func TestMySQLDefaultProjectionJobsUseSharedComposition(t *testing.T) {
	// This is an explicit business fixture; migration/recovery/permission tests
	// retain their original factory paths. No original opt-in is changed here.
	assertDefaultProjectionJobsUseSharedComposition(t, newMySQLBusinessOrderFixture(t).h)
}
func assertDefaultProjectionJobsUseSharedComposition(t *testing.T, h *handlers) {
	t.Helper()
	entered := make(chan sharedProjectionCall, 4)
	release := make(chan struct{})
	var releaseOnce sync.Once
	// Release first on test failure, before fixture cleanup waits for local work.
	defer releaseOnce.Do(func() { close(release) })
	usage := &sharedProjectionProbe{entered: entered, release: release, ready: true}
	observation := &sharedProjectionProbe{entered: entered, release: release, ready: true, runErr: errors.New("observation projection retry")}
	h.services.UsageProjections = metering.UsageProjections{Repository: sharedUsageProbe{usage}}
	h.services.NodeObservations = network.ObservationProjections{Repository: sharedObservationProbe{observation}}
	h.StartUsageProjectionWorker()
	h.StartNodeObservationWorker()
	// Repeated default starts must retain the original registration/observer.
	h.StartUsageProjectionWorker()
	h.StartNodeObservationWorker()
	seen := map[string]sharedProjectionCall{}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for len(seen) < 2 {
		select {
		case call := <-entered:
			if _, duplicate := seen[call.id]; duplicate {
				t.Fatal("same projection started twice", call.id)
			}
			seen[call.id] = call
		case <-deadline.C:
			t.Fatal("default projection job did not enter")
		}
	}
	if seen["usage_projection"].count != 200 || seen["usage_projection"].bytes != 0 || seen["node_observation"].count != 32 || seen["node_observation"].bytes != 8*1024*1024 {
		t.Fatal("default entrypoints changed bounded capability calls", seen)
	}
	// Stop scheduling while both real executions are held. A drain timeout is
	// local admission control and must neither cancel their contexts nor globally
	// revoke the durable work before the normal completion transaction.
	stop, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	err := h.services.Jobs.Drain(stop)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("held projection did not retain its in-flight drain boundary", err)
	}
	releaseOnce.Do(func() { close(release) })
	finish, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := h.services.Jobs.Drain(finish); err != nil {
		t.Fatal(err)
	}
	if usage.calls.Load() != 1 || observation.calls.Load() != 1 || usage.readyCalls.Load() == 0 || observation.readyCalls.Load() == 0 {
		t.Fatal("duplicate start/readiness contract changed")
	}
	schedules, err := jobstore.New(h.db).Schedules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, row := range schedules {
		if row.ID != "usage_projection" && row.ID != "node_observation" {
			continue
		}
		found[row.ID] = true
		wantName := "计量统计投影"
		wantState := jobs.Succeeded
		if row.ID == "node_observation" {
			wantName = "节点观察投影"
			wantState = jobs.RetryWait
		}
		if row.Name != wantName || row.Owner != "system" || row.Handler != row.ID || row.Resource != "core:"+row.ID || row.Revision != "1" || row.ExecutionGroup != "" || row.Interval != time.Second || row.Timeout != 30*time.Minute || row.MaxAttempts != 3 || row.RetryBackoff != time.Second {
			t.Fatal("durable projection policy changed", row)
		}
		if row.TimeZone != "UTC" || row.MisfirePolicy != jobs.MisfireFireOnce || row.DispatchLane != "core" || !row.FirstPlannedAt.IsZero() {
			t.Fatal("durable defaults drifted", row)
		}
		if row.State != wantState || row.LastState != wantState || row.Runs != 1 {
			t.Fatal("existing retry/outcome policy changed", row)
		}
	}
	if !found["usage_projection"] || !found["node_observation"] {
		t.Fatal("default entrypoint did not register both durable schedules")
	}
	registry := h.backgroundJobs().observations.Snapshot()
	observed := map[string]bool{}
	for _, row := range registry {
		if row.ID != "usage_projection" && row.ID != "node_observation" {
			continue
		}
		observed[row.ID] = true
		wantName := "计量统计投影"
		wantFailures := uint64(0)
		if row.ID == "node_observation" {
			wantName = "节点观察投影"
			wantFailures = 1
		}
		if row.Name != wantName || row.Kind != "periodic" || row.IntervalSeconds != 1 || row.Concurrency != 1 || row.Runs != 1 || row.Running != 0 || row.Failures != wantFailures {
			t.Fatal("handler observer wrapper drifted", row)
		}
	}
	if !observed["usage_projection"] || !observed["node_observation"] {
		t.Fatal("projection observer missing")
	}
	var attempts []jobstore.Attempt
	if err := h.db.Where("run_id IN (?)", h.db.Model(&jobstore.Record{}).Select("id").Where("handler IN ?", []string{"usage_projection", "node_observation"})).Find(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 {
		t.Fatal("repeated starts changed durable attempt count", len(attempts))
	}
}

func TestProjectionGenericReadinessDelegatesSharedDefinitions(t *testing.T) {
	sentinel := errors.New("projection readiness error")
	usage := &sharedProjectionProbe{ready: false, readyErr: sentinel}
	observation := &sharedProjectionProbe{ready: true}
	h := &handlers{services: &application.Services{UsageProjections: metering.UsageProjections{Repository: sharedUsageProbe{usage}}, NodeObservations: network.ObservationProjections{Repository: sharedObservationProbe{observation}}}}
	for _, tc := range []struct {
		id      string
		pending bool
		err     error
	}{{"usage_projection", false, sentinel}, {"node_observation", true, nil}} {
		ready := h.jobReadiness(tc.id)
		if ready == nil {
			t.Fatal("generic projection readiness was removed")
		}
		pending, err := ready(t.Context())
		if pending != tc.pending || err != tc.err {
			t.Fatal("generic readiness changed", pending, err)
		}
	}
	if h.jobReadiness("other_periodic_job") != nil {
		t.Fatal("unrelated job gained a readiness policy")
	}
}
func TestProjectionFailedRegistrationDoesNotMarkLoopActive(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	h.CloseBackgroundJobs()
	h.StartUsageProjectionWorker()
	h.StartNodeObservationWorker()
	bg := h.backgroundJobs()
	bg.mu.Lock()
	usage, observation := bg.loops["usage_projection"], bg.loops["node_observation"]
	bg.mu.Unlock()
	if usage != nil || observation != nil {
		t.Fatal("failed runtime registration was marked active")
	}
	var nilHandler *handlers
	nilHandler.StartUsageProjectionWorker()
	(&handlers{}).StartUsageProjectionWorker()
}
