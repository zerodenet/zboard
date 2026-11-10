package jobstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"testing"
	"time"
)

func TestRenewableLeaseRecoversDeadWorkerWithoutErasingUncertainty(t *testing.T) {
	a, b := fixture(t, 1)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	a.now = func() time.Time { return now }
	b.now = a.now
	d := jobs.Definition{ID: "recoverable.scan", Owner: "system", Handler: "recoverable.scan", Resource: "scan-resource", Revision: "1", Interval: time.Second, Timeout: 30 * time.Minute, Immediate: true, ReconcileAfterLoss: true}
	if err := a.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	old, err := a.ClaimRenewable(ctx, "old-worker", []string{d.Handler}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if old.ExpiresAt.Sub(now) != 30*time.Second || old.DeadlineAt.Sub(now) != 30*time.Minute {
		t.Fatal(old)
	}
	now = now.Add(31 * time.Second)
	if _, err = b.ClaimRenewable(ctx, "new-worker", []string{d.Handler}, time.Hour); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatal(err)
	}
	var attempt Attempt
	if err := a.db.First(&attempt, "token = ?", old.Token).Error; err != nil {
		t.Fatal(err)
	}
	if attempt.State != string(jobs.Unknown) || !attempt.ExpiresAt.Equal(old.DeadlineAt) {
		t.Fatal(attempt.State, attempt.ExpiresAt)
	}
	if _, err = a.RenewLease(ctx, old); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal("expired worker renewed", err)
	}
	if err = a.Finish(ctx, old, jobs.Succeeded); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal("late success accepted", err)
	}
	if err = b.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	fresh, err := b.ClaimRenewable(ctx, "new-worker", []string{d.Handler}, time.Hour)
	if err != nil || fresh.Run.ID == old.Run.ID {
		t.Fatal(fresh, err)
	}
	if err := a.db.First(&attempt, "token = ?", old.Token).Error; err != nil {
		t.Fatal(err)
	}
	if attempt.State != string(jobs.Unknown) {
		t.Fatal("recovery erased attempt evidence", attempt.State)
	}
}

func TestRenewableLeaseRenewsFenceButNeverExtendsExecutionDeadline(t *testing.T) {
	a, _ := fixture(t, 1)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	a.now = func() time.Time { return now }
	run := submit(t, a, "renew-once", "renew", "resource")
	claim, err := a.ClaimRenewable(ctx, "worker", []string{"renew"}, 35*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	deadline := claim.DeadlineAt
	now = now.Add(20 * time.Second)
	next, err := a.RenewLease(ctx, claim)
	if err != nil || !next.Equal(deadline) {
		t.Fatal(next, deadline, err)
	}
	foreign := claim
	foreign.Worker = "another-worker"
	if _, err = a.RenewLease(ctx, foreign); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal("foreign renewal", err)
	}
	now = deadline
	if _, err = a.RenewLease(ctx, claim); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal("deadline extended", err)
	}
	var attempts int64
	a.db.Model(&Attempt{}).Where("run_id = ?", run.ID).Count(&attempts)
	if attempts != 1 {
		t.Fatal("heartbeat created history rows", attempts)
	}
}

func TestRenewableLeaseDoesNotStealLegacyOrForeignLiveExecution(t *testing.T) {
	a, b := fixture(t, 1)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	a.now = func() time.Time { return now }
	b.now = a.now
	submit(t, a, "legacy-running", "legacy", "resource")
	old, err := a.Claim(ctx, "legacy-worker", []string{"legacy"}, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * time.Second)
	if _, err = b.ClaimRenewable(ctx, "new-worker", []string{"legacy"}, time.Hour); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatal("legacy lease stolen", err)
	}
	var row Record
	if err = a.db.First(&row, "id = ?", old.Run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != string(jobs.Running) || row.ExpiresAt == nil || !row.ExpiresAt.Equal(old.ExpiresAt) {
		t.Fatal(row.State, row.ExpiresAt)
	}
	if _, err = a.RenewLease(ctx, old); !errors.Is(err, jobs.ErrInvalid) {
		t.Fatal("legacy claim silently changed to renewable", err)
	}
}
