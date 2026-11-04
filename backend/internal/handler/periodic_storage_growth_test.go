package handler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestCoreScannerReusesSuccessHistoryWithoutRemovingBusinessResults(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	prepareTestMaintenanceSource(t, h)
	business := model.Task{Type: "email", Scope: `{}`, Content: `{}`, Status: 2, IdempotencyKey: "preserved-business-result"}
	if err := h.db.Create(&business).Error; err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	h.startScheduledJob("subscription_alerts", 10*time.Millisecond, func(context.Context) error { calls.Add(1); return nil })
	defer h.closeScheduledJob("subscription_alerts")
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() < 5 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() < 5 {
		t.Fatal("core scanner did not run", calls.Load())
	}
	var schedule jobstore.Schedule
	if err := h.db.Where("handler = ?", "subscription_alerts").First(&schedule).Error; err != nil || !schedule.CompactHistory {
		t.Fatal(schedule, err)
	}
	var runs int64
	if err := h.db.Model(&jobstore.Record{}).Where("schedule_id = ?", schedule.ID).Count(&runs).Error; err != nil || runs != 1 {
		t.Fatal("clean scanner ticks multiplied execution history", runs, err)
	}
	var got model.Task
	if err := h.db.First(&got, business.ID).Error; err != nil || got.Status != 2 {
		t.Fatal("periodic compaction changed domain history", got, err)
	}
}
