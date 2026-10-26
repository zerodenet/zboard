package jobstore

import (
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"gorm.io/gorm"
)

func TestContinuationDispatchReportsCommittedStateWithoutChangingIdentity(t *testing.T) {
	a, b := fixture(t, 1)
	now := time.Now().UTC().Truncate(time.Millisecond)
	a.now = func() time.Time { return now }
	b.now = a.now
	d := jobs.Definition{ID: "continuation", Owner: "system", Handler: "continuation", Revision: "1", Interval: time.Hour, Timeout: time.Minute, Immediate: true}
	outcome, err := a.ScheduleDispatch(t.Context(), d)
	if err != nil || !outcome.Runnable || !outcome.NextAt.Equal(now.Add(time.Hour)) {
		t.Fatal("new committed intent", outcome, err)
	}
	claim, err := b.Claim(t.Context(), "other-process", []string{d.Handler}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if outcome, err = a.ScheduleDispatch(t.Context(), d); err != nil || outcome.Runnable {
		t.Fatal("running lease advertised as ready", outcome, err)
	}
	if err := b.Finish(t.Context(), claim, jobs.Yielded); err != nil {
		t.Fatal(err)
	}
	if outcome, err = a.ScheduleDispatch(t.Context(), d); err != nil || outcome.Runnable || !outcome.NextAt.Equal(now.Add(100*time.Millisecond)) {
		t.Fatal("yield lost the persisted continuation deadline", outcome, err)
	}
	now = now.Add(100 * time.Millisecond)
	if outcome, err = a.ScheduleDispatch(t.Context(), d); err != nil || !outcome.Runnable {
		t.Fatal("continuation was not dispatched", outcome, err)
	}
	d.SuppressDispatch = true
	if outcome, err = a.ScheduleDispatch(t.Context(), d); err != nil || outcome.Runnable {
		t.Fatal("readiness suppression emitted wake", outcome, err)
	}
	d.SuppressDispatch = false
	var schedule Schedule
	if err := a.db.First(&schedule, "id = ?", d.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Model(&Record{}).Where("id = ?", schedule.RunID).Update("state", jobs.Unknown).Error; err != nil {
		t.Fatal(err)
	}
	if outcome, err = a.ScheduleDispatch(t.Context(), d); err != nil || outcome.Runnable {
		t.Fatal("uncertain execution emitted ready wake", outcome, err)
	}
	var count int64
	if err := a.db.Model(&Record{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatal("observation changed canonical intent identity", count, err)
	}
}

func TestContinuationDispatchDoesNotExposeRolledBackIntent(t *testing.T) {
	a, _ := fixture(t, 1)
	failure := errors.New("injected schedule checkpoint failure")
	const callback = "test:continuation-checkpoint-failure"
	if err := a.db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == (Schedule{}).TableName() {
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.db.Callback().Update().Remove(callback) })
	d := jobs.Definition{ID: "rollback", Owner: "system", Handler: "rollback", Revision: "1", Interval: time.Hour, Timeout: time.Minute, Immediate: true}
	outcome, err := a.ScheduleDispatch(t.Context(), d)
	if !errors.Is(err, failure) || outcome != (jobs.ScheduleDispatch{}) {
		t.Fatal("uncommitted intent leaked as dispatch observation", outcome, err)
	}
	for _, model := range []any{&Schedule{}, &Record{}} {
		var count int64
		if err := a.db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("failed checkpoint retained an intent", count, err)
		}
	}
}
