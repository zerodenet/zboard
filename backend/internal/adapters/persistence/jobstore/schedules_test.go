package jobstore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func TestScheduleCompetingHostsCreateOneIntentAndResumeAfterCompletion(t *testing.T) {
	a, b := fixture(t, 4)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	a.now = func() time.Time { return now }
	b.now = func() time.Time { return now }
	d := jobs.Definition{ID: "core.scan", Owner: "system", Name: "scan", Handler: "core.scan", Resource: "core.scan", Revision: "1", Interval: time.Hour, Timeout: time.Minute, Immediate: true}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, s := range []*Store{a, b} {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.Schedule(ctx, d) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	a.db.Model(&Record{}).Count(&count)
	if count != 1 {
		t.Fatal("duplicate periodic intent", count)
	}
	c, err := b.Claim(ctx, "host", []string{d.Handler}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if c.ExpiresAt.Sub(now) != time.Minute {
		t.Fatal("definition timeout ignored", c.ExpiresAt)
	}
	if err := b.Finish(ctx, c, jobs.Succeeded); err != nil {
		t.Fatal(err)
	}
	if err := a.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	rows, err := b.Schedules(ctx)
	if err != nil || len(rows) != 1 || rows[0].Runs != 1 || rows[0].LastState != jobs.Succeeded {
		t.Fatal(rows, err)
	}
	now = now.Add(time.Hour)
	if err := b.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	a.db.Model(&Record{}).Count(&count)
	if count != 2 {
		t.Fatal("restart lost next intent", count)
	}
}

func TestOnlyExplicitReconcilersResumeAfterLeaseLoss(t *testing.T) {
	a, _ := fixture(t, 1)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	a.now = func() time.Time { return now }
	d := jobs.Definition{ID: "reconcile", Owner: "system", Handler: "reconcile", Resource: "resource", Revision: "1", Interval: time.Second, Timeout: time.Second, Immediate: true}
	if err := a.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	c, err := a.Claim(ctx, "old", []string{d.Handler}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if _, err := a.Claim(ctx, "new", []string{d.Handler}, time.Minute); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatal(err)
	}
	if err := a.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	var old Record
	a.db.First(&old, "id = ?", c.Run.ID)
	if old.State != string(jobs.Unknown) {
		t.Fatal(old.State)
	}
	d.ReconcileAfterLoss = true
	if err := a.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	a.db.First(&old, "id = ?", c.Run.ID)
	if old.State != string(jobs.Interrupted) {
		t.Fatal(old.State)
	}
	var attempt Attempt
	a.db.First(&attempt, "token = ?", c.Token)
	if attempt.State != string(jobs.Unknown) {
		t.Fatal("uncertainty was erased", attempt.State)
	}
	now = now.Add(time.Second)
	if err := a.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Claim(ctx, "new", []string{d.Handler}, time.Minute); err != nil {
		t.Fatal(err)
	}
}

func TestYieldedBatchReschedulesBeforeLongIdleInterval(t *testing.T) {
	a, _ := fixture(t, 1)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	a.now = func() time.Time { return now }
	d := jobs.Definition{ID: "events", Owner: "system", Handler: "events", Revision: "1", Interval: time.Hour, Timeout: time.Minute, Immediate: true}
	if err := a.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	c, err := a.Claim(ctx, "host", []string{"events"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Finish(ctx, c, jobs.Yielded); err != nil {
		t.Fatal(err)
	}
	if err := a.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	rows, err := a.Schedules(ctx)
	if err != nil || rows[0].NextAt.Sub(now) > time.Second || rows[0].Failures != 0 {
		t.Fatal(rows, err)
	}
}

func TestSchedulePreservesPlannedCadenceAndCollapsesMisfires(t *testing.T) {
	a, _ := fixture(t, 1)
	ctx := context.Background()
	start := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	now := start
	a.now = func() time.Time { return now }
	d := jobs.Definition{ID: "planned", Owner: "system", Handler: "planned", Revision: "1", Interval: time.Minute, Timeout: time.Minute, TimeZone: "Asia/Shanghai", MisfirePolicy: jobs.MisfireFireOnce, FirstPlannedAt: start}
	if err := a.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	first, err := a.Claim(ctx, "host", []string{d.Handler}, time.Minute)
	if err != nil || first.Run.PlannedAt == nil || !first.Run.PlannedAt.Equal(start) {
		t.Fatal(first, err)
	}
	now = start.Add(40 * time.Second)
	if err := a.Finish(ctx, first, jobs.Succeeded); err != nil {
		t.Fatal(err)
	}
	if err := a.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := a.db.Model(&Record{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
	now = start.Add(3*time.Minute + 20*time.Second)
	if err := a.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	second, err := a.Claim(ctx, "host", []string{d.Handler}, time.Minute)
	if err != nil || second.Run.PlannedAt == nil || !second.Run.PlannedAt.Equal(start.Add(3*time.Minute)) {
		t.Fatal(second, err)
	}
	views, err := a.Schedules(ctx)
	if err != nil || len(views) != 1 || views[0].MissedRuns != 2 || !views[0].NextAt.Equal(start.Add(4*time.Minute)) || views[0].TimeZone != "Asia/Shanghai" {
		t.Fatal(views, err)
	}
	if err := a.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Model(&Record{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatal("planned slot was duplicated", count, err)
	}
	planned := start.Add(3 * time.Minute)
	duplicate := Record{ID: "duplicate-planned-slot", Owner: "system", Key: "duplicate-planned-slot", Handler: d.Handler, Payload: `{}`, Fingerprint: "duplicate", State: string(jobs.Queued), NotBefore: now, CreatedAt: now, ScheduleID: d.ID, PlannedAt: &planned, DispatchLane: "core", MaxAttempts: 1}
	if err := a.db.Create(&duplicate).Error; err == nil {
		t.Fatal("schedule_id + planned_at unique trigger was not enforced")
	}
}

func TestScheduleSkipMisfireAndTimezoneValidation(t *testing.T) {
	a, _ := fixture(t, 1)
	ctx := context.Background()
	start := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	now := start.Add(3*time.Minute + 20*time.Second)
	a.now = func() time.Time { return now }
	d := jobs.Definition{ID: "skip", Owner: "system", Handler: "skip", Revision: "1", Interval: time.Minute, Timeout: time.Second, TimeZone: "UTC", MisfirePolicy: jobs.MisfireSkip, FirstPlannedAt: start}
	if err := a.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := a.db.Model(&Record{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal(count, err)
	}
	views, err := a.Schedules(ctx)
	if err != nil || len(views) != 1 || views[0].MissedRuns != 4 || !views[0].NextAt.Equal(start.Add(4*time.Minute)) {
		t.Fatal(views, err)
	}
	d.ID, d.Handler, d.TimeZone = "invalid-zone", "invalid-zone", "Mars/Olympus"
	if err := a.Schedule(ctx, d); !errors.Is(err, jobs.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestCancelHandlerTerminatesPendingAndRequestsActiveCancellation(t *testing.T) {
	a, _ := fixture(t, 2)
	ctx := context.Background()
	d := jobs.Definition{ID: "plugin:test:task", Owner: "plugin:test", Handler: "plugin:test:task", Revision: "1", Interval: time.Hour, Timeout: time.Minute, Immediate: true}
	if err := a.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := a.CancelHandler(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	runs, err := a.List(ctx, d.Owner, 10, 0)
	if err != nil || len(runs) != 1 || runs[0].State != jobs.Canceled {
		t.Fatal(runs, err)
	}
	rows, err := a.Schedules(ctx)
	if err != nil || rows[0].Runs != 0 {
		t.Fatal(rows, err)
	}
	var attempts int64
	if err := a.db.Model(&Attempt{}).Count(&attempts).Error; err != nil || attempts != 0 {
		t.Fatal(attempts, err)
	}
	if _, err := a.Claim(ctx, "host", []string{d.ID}, time.Minute); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatal(err)
	}
	d.ID = "plugin:test:active"
	d.Handler = d.ID
	if err := a.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	c, err := a.Claim(ctx, "host", []string{d.ID}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CancelHandler(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	var active Record
	if err := a.db.First(&active, "id = ?", c.Run.ID).Error; err != nil || active.State != string(jobs.CancelRequested) {
		t.Fatal(active, err)
	}
	requested, err := a.CancellationRequested(ctx, c)
	if err != nil || !requested {
		t.Fatal("active cancellation was not observable", requested, err)
	}
	// Completion may win the cancellation race and remains authoritative.
	if err := a.Finish(ctx, c, jobs.Succeeded); err != nil {
		t.Fatal(err)
	}
}

func TestQueueStatusIncludesBothSourcesAndExpiredExecution(t *testing.T) {
	a, _ := fixture(t, 2)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	a.now = func() time.Time { return now }
	for _, v := range []jobs.Submission{
		{Owner: "system", Key: "core", Handler: "core", Payload: "{}"},
		{Owner: "plugin:test", Key: "plugin", Handler: "plugin", Payload: "{}"},
		{Owner: "plugin:test", Key: "later", Handler: "later", Payload: "{}", NotBefore: now.Add(time.Hour)},
	} {
		if _, err := a.Submit(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.Claim(ctx, "host", []string{"core"}, time.Minute); err != nil {
		t.Fatal(err)
	}
	q, err := a.QueueStatus(ctx)
	if err != nil || q.Pending != 1 || q.Running != 1 || q.Delayed != 1 || q.Unknown != 0 {
		t.Fatal(q, err)
	}
	now = now.Add(2 * time.Minute)
	q, err = a.QueueStatus(ctx)
	if err != nil || q.Pending != 1 || q.Running != 0 || q.Delayed != 1 || q.Unknown != 1 {
		t.Fatal(q, err)
	}
}

func TestSubmittedTimeoutSurvivesRestartAndFencesLateCompletion(t *testing.T) {
	a, b := fixture(t, 1)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	a.now = func() time.Time { return now }
	b.now = func() time.Time { return now }
	in := jobs.Submission{Owner: "system", Key: "dns:1", Handler: "dns", Resource: "dns:1", Payload: "{}", Timeout: time.Minute}
	run, err := a.Submit(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	again, err := b.Submit(ctx, in)
	if err != nil || again.ID != run.ID || again.Timeout != time.Minute {
		t.Fatal(again, err)
	}
	in.Timeout = 2 * time.Minute
	if _, err := b.Submit(ctx, in); !errors.Is(err, jobs.ErrConflict) {
		t.Fatal(err)
	}
	c, err := b.Claim(ctx, "restarted", []string{"dns"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !c.ExpiresAt.Equal(now.Add(time.Minute)) {
		t.Fatal("worker default replaced durable timeout", c.ExpiresAt)
	}
	now = now.Add(61 * time.Second)
	if err := a.Finish(ctx, c, jobs.Succeeded); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal(err)
	}
}

func TestSubmissionRejectsInvalidTimeout(t *testing.T) {
	a, _ := fixture(t, 1)
	for _, timeout := range []time.Duration{-time.Second, time.Nanosecond, time.Millisecond + time.Nanosecond, 2 * time.Hour} {
		_, err := a.Submit(context.Background(), jobs.Submission{Owner: "system", Key: "bad", Handler: "test", Payload: "{}", Timeout: timeout})
		if !errors.Is(err, jobs.ErrInvalid) {
			t.Fatal(timeout, err)
		}
	}
}
