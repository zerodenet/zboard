package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"testing"
	"time"
)

func TestProxyPoolWorkerRecoversLostScheduleWithoutManualReset(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	assertProxyPoolWorkerRecoversLostSchedule(t, h)
}

func TestMySQLProxyPoolWorkerRecoversLostScheduleWithoutManualReset(t *testing.T) {
	assertProxyPoolWorkerRecoversLostSchedule(t, newMySQLBusinessOrderFixture(t).h)
}

func assertProxyPoolWorkerRecoversLostSchedule(t *testing.T, h *handlers) {
	t.Helper()
	prepareTestMaintenanceSource(t, h)
	store := jobstore.New(h.db)
	ctx := context.Background()
	d := jobs.Definition{ID: "proxy_pool_sync", Owner: "system", Name: "old sync", Handler: "proxy_pool_sync", Resource: "core:proxy_pool_sync", Revision: "1", Interval: time.Minute, Timeout: time.Millisecond, Immediate: true}
	if e := store.Schedule(ctx, d); e != nil {
		t.Fatal(e)
	}
	lease, e := store.Claim(ctx, "lost-process", []string{d.Handler}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	time.Sleep(10 * time.Millisecond)
	if _, e := store.Claim(ctx, "reaper", []string{d.Handler}, time.Minute); !errors.Is(e, jobs.ErrEmpty) {
		t.Fatal(e)
	}
	var old jobstore.Record
	if e := h.db.First(&old, "id=?", lease.Run.ID).Error; e != nil || old.State != string(jobs.Unknown) {
		t.Fatal("fixture did not reproduce stuck schedule", old.State, e)
	}
	ran := make(chan struct{}, 1)
	h.startScheduledJob("proxy_pool_sync", time.Minute, func(context.Context) error {
		select {
		case ran <- struct{}{}:
		default:
		}
		return nil
	})
	t.Cleanup(h.CloseBackgroundJobs)
	select {
	case <-ran:
	case <-time.After(8 * time.Second):
		t.Fatal("proxy pool scan remained stuck after registration")
	}
	if e := h.db.First(&old, "id=?", lease.Run.ID).Error; e != nil || old.State != string(jobs.Interrupted) {
		t.Fatal("lost run not reconciled", old.State, e)
	}
	var attempt jobstore.Attempt
	if e := h.db.First(&attempt, "token=?", lease.Token).Error; e != nil || attempt.State != string(jobs.Unknown) {
		t.Fatal("old uncertainty erased", attempt.State, e)
	}
	var schedule jobstore.Schedule
	if e := h.db.First(&schedule, "id=?", d.ID).Error; e != nil || !schedule.CompactHistory || schedule.MaxAttempts != 3 || schedule.TimeoutMS != 120000 {
		t.Fatal("recovery policy missing", schedule, e)
	}
}
