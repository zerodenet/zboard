package jobstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"testing"
	"time"
)

func TestCompactPeriodicSuccessBoundedAndOldCompletionFenced(t *testing.T) {
	a, b := fixture(t, 1)
	ctx := context.Background()
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	b.now = a.now
	d := jobs.Definition{ID: "projection", Owner: "system", Handler: "projection", Resource: "projection", Revision: "1", Interval: time.Second, Timeout: time.Second, Immediate: true, ReconcileAfterLoss: true, CompactHistory: true}
	var first jobs.Claim
	for i := 0; i < 1000; i++ {
		if err := a.Schedule(ctx, d); err != nil {
			t.Fatal(i, err)
		}
		c, err := b.Claim(ctx, "worker", []string{d.Handler}, time.Minute)
		if err != nil {
			t.Fatal(i, err)
		}
		if i == 0 {
			first = c
		} else {
			if c.Run.ID != first.Run.ID || c.Token == first.Token {
				t.Fatal("slot or token", i, c)
			}
			if err := a.Finish(ctx, first, jobs.Succeeded); !errors.Is(err, jobs.ErrLeaseLost) {
				t.Fatal("old completion crossed generation", err)
			}
			if _, err := a.Claim(ctx, "other", []string{d.Handler}, time.Minute); !errors.Is(err, jobs.ErrEmpty) {
				t.Fatal("old completion released capacity", err)
			}
		}
		if err := b.Finish(ctx, c, jobs.Succeeded); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
	}
	for _, table := range []string{"job_runs", "job_attempts"} {
		var n int64
		if err := a.db.Table(table).Count(&n).Error; err != nil || n != 1 {
			t.Fatal(table, n, err)
		}
	}
	schedules, err := a.Schedules(ctx)
	if err != nil || len(schedules) != 1 || schedules[0].Runs != 1000 || !schedules[0].CompactHistory {
		t.Fatal(schedules, err)
	}
	// A dormant slot survives ordinary success-history retention.
	d.SuppressDispatch = true
	if err := a.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	if n, err := a.PrunePeriodicOutcome(ctx, now, true); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	d.SuppressDispatch = false
	d.Revision = "2"
	if err := a.Schedule(ctx, d); err != nil {
		t.Fatal(err)
	}
	c, err := a.Claim(ctx, "worker", []string{d.Handler}, time.Minute)
	if err != nil || c.Run.ID == first.Run.ID {
		t.Fatal("definition change reused old identity", c, err)
	}
}
func TestCompactPeriodicPreservesFailuresRetriesAndUncertainty(t *testing.T) {
	for _, outcome := range []jobs.State{jobs.Failed, jobs.Unknown, jobs.RetryWait} {
		t.Run(string(outcome), func(t *testing.T) {
			a, _ := fixture(t, 1)
			ctx := context.Background()
			now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
			a.now = func() time.Time { return now }
			d := jobs.Definition{ID: "projection", Owner: "system", Handler: "projection", Resource: "projection", Revision: "1", Interval: time.Second, Timeout: time.Second, Immediate: true, ReconcileAfterLoss: true, CompactHistory: true, MaxAttempts: 3, RetryBackoff: time.Second}
			if err := a.Schedule(ctx, d); err != nil {
				t.Fatal(err)
			}
			c, err := a.Claim(ctx, "worker", []string{d.Handler}, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Finish(ctx, c, outcome); err != nil {
				t.Fatal(err)
			}
			now = now.Add(2 * time.Second)
			if outcome == jobs.RetryWait {
				c2, err := a.Claim(ctx, "worker", []string{d.Handler}, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				if err := a.Finish(ctx, c2, jobs.Succeeded); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.Schedule(ctx, d); err != nil {
				t.Fatal(err)
			}
			next, err := a.Claim(ctx, "worker", []string{d.Handler}, time.Minute)
			if err != nil || next.Run.ID == c.Run.ID {
				t.Fatal("evidence reused", next, err)
			}
			var attempt Attempt
			if err := a.db.First(&attempt, "token = ?", c.Token).Error; err != nil || attempt.State != string(outcome) {
				t.Fatal(attempt, err)
			}
		})
	}
}
func TestCompactHistoryRejectsPluginAndExternalDefinitions(t *testing.T) {
	a, _ := fixture(t, 1)
	for _, d := range []jobs.Definition{{ID: "plugin", Owner: "plugin:x", Handler: "x", Interval: time.Second, Timeout: time.Second, CompactHistory: true, ReconcileAfterLoss: true}, {ID: "external", Owner: "system", Handler: "x", ExecutionGroup: "external", Interval: time.Second, Timeout: time.Second, CompactHistory: true, ReconcileAfterLoss: true}} {
		if err := a.Schedule(context.Background(), d); !errors.Is(err, jobs.ErrInvalid) {
			t.Fatal(err)
		}
	}
}
