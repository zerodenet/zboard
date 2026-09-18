package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
	"time"
)

func TestBatchLifecycleRecoveryAndCompletionFences(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	ctx := context.Background()
	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)
	tasks := []model.Task{
		{Type: "email", Content: `{}`, Scope: `{}`, Status: 1, LockedBy: "expired-owner", LockedUntil: &past, MaxAttempts: 3, IdempotencyKey: "lifecycle-expired"},
		{Type: "quota", Content: `{}`, Scope: `{}`, Status: 0, ScheduledAt: &past, MaxAttempts: 3, IdempotencyKey: "lifecycle-ready"},
		{Type: "quota", Content: `{}`, Scope: `{}`, Status: 0, MaxAttempts: 3, IdempotencyKey: "lifecycle-draft"},
		{Type: "database_migration", Content: `{}`, Scope: `{}`, Status: 1, LockedBy: "maintenance-owner", LockedUntil: &past, MaxAttempts: 3, IdempotencyKey: "lifecycle-maintenance"},
		{Type: "quota", Content: `{}`, Scope: `{}`, Status: 1, LockedBy: "current-owner", LockedUntil: &future, MaxAttempts: 3, IdempotencyKey: "lifecycle-current"},
	}
	for i := range tasks {
		if err := h.db.Create(&tasks[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := h.services.BatchLifecycle()
	ids, err := service.Ready(ctx, 1)
	if err != nil || len(ids) != 1 || ids[0] != tasks[1].ID {
		t.Fatal(ids, err)
	}
	var expired, maintenance model.Task
	if err := h.db.First(&expired, tasks[0].ID).Error; err != nil || expired.Status != 3 || expired.LockedBy != "" {
		t.Fatal(expired, err)
	}
	if err := h.db.First(&maintenance, tasks[3].ID).Error; err != nil || maintenance.Status != 1 {
		t.Fatal("maintenance changed", maintenance, err)
	}
	if err := service.Renew(ctx, tasks[0].ID, "expired-owner"); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal("expired lease renewed", err)
	}
	if err := service.Finish(ctx, tasks[4].ID, "old-owner", nil); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal("old owner finished", err)
	}
	if err := service.Renew(ctx, tasks[4].ID, "current-owner"); err != nil {
		t.Fatal(err)
	}
	item := model.TaskItem{TaskID: tasks[4].ID, TargetType: "subscription", TargetID: "1", Payload: `{}`, Status: 0}
	if err := h.db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Finish(ctx, tasks[4].ID, "current-owner", nil); err != nil {
		t.Fatal(err)
	}
	var result model.Task
	if err := h.db.First(&result, tasks[4].ID).Error; err != nil || result.Status != 3 || result.LockedBy != "" || result.Errors == "" {
		t.Fatal("incomplete targets reported success", result, err)
	}
	if err := service.Finish(ctx, tasks[4].ID, "current-owner", nil); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal("terminal result overwritten", err)
	}
}
