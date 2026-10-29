package jobstore

import (
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func TestProjectionActivityUsesCurrentCrossProcessAttemptAndIgnoresOtherWork(t *testing.T) {
	a, b := fixture(t, 4)
	now := time.Now().UTC().Truncate(time.Millisecond)
	a.now = func() time.Time { return now }
	b.now = a.now
	handlers := []string{"usage_projection", "node_observation"}
	foreign, err := a.Submit(t.Context(), jobs.Submission{Owner: "plugin:outside", Key: "outside", Handler: "outside", Resource: "outside", Payload: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Claim(t.Context(), "outside-worker", []string{foreign.Handler}, time.Hour); err != nil {
		t.Fatal(err)
	}
	activity := InspectProjectionActivity(t.Context(), a.db, handlers, now)
	if !activity.Known || activity.Active != 0 || activity.Longest != 0 {
		t.Fatal("foreign work entered projection pressure", activity)
	}
	run, err := a.Submit(t.Context(), jobs.Submission{Owner: "system", Key: "owned-projection", Handler: handlers[0], Resource: "core:" + handlers[0], Payload: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := b.Claim(t.Context(), "actual-separate-worker", []string{run.Handler}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	activity = InspectProjectionActivity(t.Context(), a.db, handlers, now.Add(3*time.Second))
	if !activity.Known || activity.Active != 1 || activity.Longest != 3*time.Second {
		t.Fatal("child in-flight time missing", activity)
	}
	if err := b.Finish(t.Context(), claim, jobs.Succeeded); err != nil {
		t.Fatal(err)
	}
	activity = InspectProjectionActivity(t.Context(), a.db, handlers, now.Add(4*time.Second))
	if !activity.Known || activity.Active != 0 {
		t.Fatal("finished attempt remained in-flight", activity)
	}
}

func TestProjectionActivityUnknownAndCorruptAttemptNeverGrantsCapacity(t *testing.T) {
	a, b := fixture(t, 2)
	now := time.Now().UTC().Truncate(time.Millisecond)
	a.now = func() time.Time { return now }
	b.now = a.now
	handlers := []string{"usage_projection", "node_observation"}
	run, err := a.Submit(t.Context(), jobs.Submission{Owner: "system", Key: "uncertain", Handler: handlers[1], Resource: "core:" + handlers[1], Payload: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := b.Claim(t.Context(), "actual-separate-worker", []string{run.Handler}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.Model(&Attempt{}).Where("token = ?", claim.Token).Update("worker", "different-worker").Error; err != nil {
		t.Fatal(err)
	}
	if activity := InspectProjectionActivity(t.Context(), a.db, handlers, now); activity.Known {
		t.Fatal("mismatched current attempt became capacity evidence", activity)
	}
	if err := a.db.Model(&Attempt{}).Where("token = ?", claim.Token).Update("worker", claim.Worker).Error; err != nil {
		t.Fatal(err)
	}
	if err := b.Finish(t.Context(), claim, jobs.Unknown); err != nil {
		t.Fatal(err)
	}
	if activity := InspectProjectionActivity(t.Context(), a.db, handlers, now); activity.Known {
		t.Fatal("unresolved resource fence became capacity evidence", activity)
	}
}
