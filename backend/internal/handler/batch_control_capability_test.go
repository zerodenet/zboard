package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"testing"
)

func TestBatchControlCurrentAuthorityAuditAndClaim(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	ctx := context.Background()
	admin := model.User{Email: "batch-controller@example.test", Password: "unused", IsAdmin: true, Status: "active"}
	if err := h.db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{Type: "quota", Content: `{}`, Scope: `{}`, Status: 3, Total: 2, MaxAttempts: 3, IdempotencyKey: "batch-control-fixture"}
	if err := h.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	items := []model.TaskItem{{TaskID: task.ID, TargetType: "subscription", TargetID: "1", Payload: `{}`, Status: 2}, {TaskID: task.ID, TargetType: "subscription", TargetID: "2", Payload: `{}`, Status: 3}}
	if err := h.db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	service := h.services.BatchControl()
	if err := service.Queue(ctx, 0, task.ID); !errors.Is(err, jobs.ErrBatchPermission) {
		t.Fatal(err)
	}
	const callback = "batch-run-audit-failure"
	failure := errors.New("fixture audit failure")
	if err := h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_logs" {
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	err := service.Queue(ctx, admin.ID, task.ID)
	h.db.Callback().Create().Remove(callback)
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	var current model.Task
	if err := h.db.First(&current, task.ID).Error; err != nil || current.Status != 3 || current.ScheduledAt != nil {
		t.Fatal("queue escaped rollback", current, err)
	}
	if err := service.Queue(ctx, admin.ID, task.ID); err != nil {
		t.Fatal(err)
	}
	token, err := h.services.ClaimBatch(ctx, 0, task.ID, true)
	if err != nil || token == "" {
		t.Fatal(token, err)
	}
	if err := h.db.First(&current, task.ID).Error; err != nil || current.Status != 1 || current.Current != 1 || current.Attempts != 1 || current.LockedBy != token {
		t.Fatal(current, err)
	}
	if token, err := h.services.ClaimBatch(ctx, 0, task.ID, true); err == nil || token != "" {
		t.Fatal("duplicate claim returned authority", token, err)
	}
	if err := h.db.Model(&admin).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Queue(ctx, admin.ID, task.ID); !errors.Is(err, jobs.ErrBatchPermission) {
		t.Fatal("revoked actor queued", err)
	}
}
