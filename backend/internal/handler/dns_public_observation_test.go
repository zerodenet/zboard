package handler

import (
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func TestDNSPublicObservationWorkerRecoversAnUnknownRun(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	prepareTestMaintenanceSource(t, h)
	ctx := t.Context()
	store := jobstore.New(h.db)
	const id = "dns_observation"
	previous := jobs.Definition{
		ID: id, Name: jobNames[id], Handler: id, Owner: "system", Resource: "core:" + id,
		Revision: "1", Interval: dnsPublicCheckInterval, Timeout: 30 * time.Minute, Immediate: true,
	}
	if err := store.Schedule(ctx, previous); err != nil {
		t.Fatal(err)
	}
	var schedule jobstore.Schedule
	if err := h.db.First(&schedule, "id = ?", id).Error; err != nil || schedule.RunID == "" {
		t.Fatalf("previous observation schedule=%+v error=%v", schedule, err)
	}
	if err := h.db.Model(&jobstore.Record{}).Where("id = ?", schedule.RunID).Update("state", jobs.Unknown).Error; err != nil {
		t.Fatal(err)
	}

	h.StartDNSPublicObservationWorker()
	deadline := time.Now().Add(4 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		var old, next jobstore.Record
		if err := h.db.First(&old, "id = ?", schedule.RunID).Error; err != nil {
			t.Fatal(err)
		}
		result := h.db.Where("schedule_id = ? AND id <> ? AND state = ?", id, old.ID, jobs.Succeeded).Limit(1).Find(&next)
		if result.Error != nil {
			t.Fatal(result.Error)
		}
		if old.State == string(jobs.Interrupted) && result.RowsAffected == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("observation worker did not replace the unknown run: old=%s next=%s", old.State, next.State)
		}
	}
}
