package handler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

const periodicPrivatePanic = "private-panic-material-must-not-reach-observation"
const periodicSafePanic = "job handler did not return; result requires verification"

type periodicPanicProbe struct {
	entered                 chan struct{}
	release                 <-chan struct{}
	calls, panics, canceled atomic.Int32
}

func (p *periodicPanicProbe) HasPending(context.Context) (bool, error) { return true, nil }
func (p *periodicPanicProbe) run(ctx context.Context) error {
	if p.calls.Add(1) == 1 {
		close(p.entered)
	}
	select {
	case <-p.release:
		p.panics.Add(1)
		panic(periodicPrivatePanic)
	case <-ctx.Done():
		p.canceled.Add(1)
		return ctx.Err()
	}
}

type periodicPanicUsage struct{ *periodicPanicProbe }

func (p periodicPanicUsage) Drain(ctx context.Context, _ int) (int, error) {
	return 0, p.run(ctx)
}

type periodicPanicObservation struct{ *periodicPanicProbe }

func (p periodicPanicObservation) Drain(ctx context.Context, _ int, _ int64) (int, error) {
	return 0, p.run(ctx)
}

func TestSQLitePeriodicPanicRetainsUnknownAndFailedObservation(t *testing.T) {
	assertPeriodicPanicRetainsUnknownAndFailedObservation(t, func(t *testing.T) *handlers {
		h, _ := newAnnouncementTestHandlers(t)
		prepareTestMaintenanceSource(t, h)
		return h
	})
}

func TestMySQLPeriodicPanicRetainsUnknownAndFailedObservation(t *testing.T) {
	assertPeriodicPanicRetainsUnknownAndFailedObservation(t, func(t *testing.T) *handlers {
		return newMySQLBusinessOrderFixture(t).h
	})
}

func assertPeriodicPanicRetainsUnknownAndFailedObservation(t *testing.T, fixture func(*testing.T) *handlers) {
	t.Helper()
	for _, kind := range []string{"generic", "usage_projection", "node_observation"} {
		t.Run(kind, func(t *testing.T) {
			h := fixture(t)
			release := make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			p := &periodicPanicProbe{entered: make(chan struct{}), release: release}
			id := kind
			switch kind {
			case "generic":
				id = "subscription_alerts"
				h.startScheduledJob(id, time.Second, p.run)
			case "usage_projection":
				h.services.UsageProjections = metering.UsageProjections{Repository: periodicPanicUsage{p}}
				h.StartUsageProjectionWorker()
			case "node_observation":
				h.services.NodeObservations = network.ObservationProjections{Repository: periodicPanicObservation{p}}
				h.StartNodeObservationWorker()
			}
			select {
			case <-p.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("periodic handler did not enter")
			}
			// Stop local admission while retaining the real in-flight handler.
			// The panic must reach Executor's existing Unknown completion path.
			budget, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			err := h.services.Jobs.Drain(budget)
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("held execution lost the local drain boundary", err)
			}
			releaseOnce.Do(func() { close(release) })
			finished, cancelFinished := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancelFinished()
			if err := h.services.Jobs.Drain(finished); err != nil {
				t.Fatal("panic completion was not durably recorded", err)
			}
			if p.calls.Load() != 1 || p.panics.Load() != 1 || p.canceled.Load() != 0 {
				t.Fatal("panic was canceled, swallowed or executed repeatedly")
			}
			var records []jobstore.Record
			if err := h.db.Where("handler = ? AND schedule_id <> ''", id).Find(&records).Error; err != nil || len(records) != 1 {
				t.Fatal("expected one durable execution", len(records), err)
			}
			run := records[0]
			var attempts []jobstore.Attempt
			if err := h.db.Where("run_id = ?", run.ID).Find(&attempts).Error; err != nil || len(attempts) != 1 {
				t.Fatal("expected one durable attempt", len(attempts), err)
			}
			var schedule jobstore.Schedule
			if err := h.db.First(&schedule, "id = ?", id).Error; err != nil {
				t.Fatal(err)
			}
			if run.State != string(jobs.Unknown) || attempts[0].State != string(jobs.Unknown) || schedule.State != string(jobs.Unknown) || schedule.LastState != string(jobs.Unknown) || run.AttemptCount != 1 || schedule.Runs != 1 {
				t.Fatal("panic changed Unknown into success or retry", run.State, attempts[0].State, schedule.State, schedule.LastState, run.AttemptCount, schedule.Runs)
			}
			for _, row := range h.backgroundJobs().observations.Snapshot() {
				if row.ID != id {
					continue
				}
				if row.Runs != 1 || row.Running != 0 || row.Failures != 1 || row.LastResult != "failed" || row.LastError != periodicSafePanic || strings.Contains(row.LastError, periodicPrivatePanic) {
					t.Fatalf("panic observation must fail safely while durable execution stays unknown: %+v", row)
				}
				return
			}
			t.Fatal("periodic panic observation missing")
		})
	}
}
