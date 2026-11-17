package jobstore

import (
	"os"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func TestProjectionStatisticsActivityIncludesFiveActualDurableWorkers(t *testing.T) {
	a, b := fixture(t, 6)
	assertStatisticsActivity(t, a, b)
}
func TestMySQLProjectionStatisticsActivityIncludesFiveActualDurableWorkers(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("isolated MySQL opt-in is not configured")
	}
	a, b := mysqlFixture(t, 6)
	assertStatisticsActivity(t, a, b)
}
func assertStatisticsActivity(t *testing.T, a, b *Store) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	a.now = func() time.Time { return now.Add(-10 * time.Second) }
	b.now = a.now
	handlers := []string{"usage_projection", "node_observation", "principal_statistics", "principal_scope_retention", "flow_start_statistics"}
	claims := make([]jobs.Claim, 0, 5)
	for _, handler := range handlers {
		run, err := a.Submit(t.Context(), jobs.Submission{Owner: "system", Key: "activity:" + handler, Handler: handler, Resource: "core:" + handler, Payload: `{}`})
		if err != nil {
			t.Fatal(err)
		}
		claim, err := b.Claim(t.Context(), "child:"+handler, []string{run.Handler}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		claims = append(claims, claim)
	}
	activity := InspectProjectionActivity(t.Context(), New(a.db).db, handlers, now)
	if !activity.Known || activity.Active != 5 || activity.Longest != 10*time.Second {
		t.Fatal("new independent workers absent after local observer restart", activity)
	}
	// Corrupt duplicate active ownership must remain unknown even with the
	// larger inventory; the fixed LIMIT detects one extra row rather than
	// allowing five task types to hide an overflow.
	var extra Record
	if err := a.db.First(&extra, "id = ?", claims[2].Run.ID).Error; err != nil {
		t.Fatal(err)
	}
	extra.ID, extra.Key, extra.Token = "capacity-overflow", "capacity-overflow", "capacity-overflow-token"
	if err := a.db.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}
	if got := InspectProjectionActivity(t.Context(), a.db, handlers, now); got.Known {
		t.Fatal("extra active ownership was hidden", got)
	}
	if err := a.db.Where("id = ?", extra.ID).Delete(&Record{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := b.Finish(t.Context(), claims[2], jobs.Succeeded); err != nil {
		t.Fatal(err)
	}
	if got := InspectProjectionActivity(t.Context(), a.db, handlers, now); !got.Known || got.Active != 4 {
		t.Fatal("finished statistics remained active", got)
	}
	if err := b.Finish(t.Context(), claims[3], jobs.Unknown); err != nil {
		t.Fatal(err)
	}
	if activity := InspectProjectionActivity(t.Context(), a.db, handlers, now); activity.Known {
		t.Fatal("unknown retention fence permitted expansion", activity)
	}
}
func TestProjectionActivityRejectsInvalidBoundedInventory(t *testing.T) {
	a, _ := fixture(t, 4)
	for _, handlers := range [][]string{nil, {}, {""}, {"principal_statistics", "principal_statistics"}, {"usage_projection", "node_observation", "principal_statistics", "principal_scope_retention", "flow_start_statistics", "future"}} {
		if activity := InspectProjectionActivity(t.Context(), a.db, handlers, time.Now().UTC()); activity.Known {
			t.Fatal("invalid inventory became idle evidence", handlers, activity)
		}
	}
}
