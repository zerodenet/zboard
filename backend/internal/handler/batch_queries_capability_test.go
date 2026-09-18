package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
)

func TestBatchQueriesPreserveCountsAndCurrentAuthority(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	ctx := context.Background()
	admin := model.User{Email: "batch-reader@example.test", Password: "unused", Status: "active", IsAdmin: true}
	if err := h.db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{Type: "email", Content: `{}`, Scope: `{}`, Total: 2, Current: 1, Status: 1, IdempotencyKey: "batch-query-fixture"}
	if err := h.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	items := []model.TaskItem{{TaskID: task.ID, TargetType: "user", TargetID: "1", Payload: `{}`, Status: 0}, {TaskID: task.ID, TargetType: "user", TargetID: "2", Payload: `{}`, Status: 2}}
	if err := h.db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	service := h.services.BatchQueries()
	filter := jobs.BatchFilter{Type: "email", Limit: 1}
	page, err := service.List(ctx, admin.ID, filter)
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].PendingCount != 1 || page.Items[0].SucceededCount != 1 || page.Items[0].ID != task.ID {
		t.Fatal(page, err)
	}
	detail, err := service.Detail(ctx, admin.ID, task.ID, true)
	if err != nil || len(detail.Items) != 2 || detail.Total != 2 {
		t.Fatal(detail, err)
	}
	status := int16(2)
	targetPage, err := service.Items(ctx, admin.ID, task.ID, jobs.BatchFilter{Status: &status, Limit: 1})
	if err != nil || targetPage.Total != 1 || len(targetPage.Items) != 1 || targetPage.Items[0].TargetID != "2" {
		t.Fatal(targetPage, err)
	}
	summary, err := service.Summary(ctx, admin.ID)
	if err != nil || summary.Running != 1 || summary.ActiveTotal != 2 || summary.ActiveCurrent != 1 || summary.PendingTargets != 1 || summary.SucceededTargets != 1 {
		t.Fatal(summary, err)
	}
	if _, err := service.Detail(ctx, admin.ID, task.ID+1, false); !errors.Is(err, jobs.ErrBatchNotFound) {
		t.Fatal(err)
	}
	if err := h.db.Model(&admin).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	checks := []func() error{func() error { _, e := service.List(ctx, admin.ID, filter); return e }, func() error { _, e := service.Detail(ctx, admin.ID, task.ID, true); return e }, func() error { _, e := service.Items(ctx, admin.ID, task.ID, filter); return e }, func() error { _, e := service.Summary(ctx, admin.ID); return e }}
	for _, check := range checks {
		if err := check(); !errors.Is(err, jobs.ErrBatchPermission) {
			t.Fatal("revoked reader accepted", err)
		}
	}
}
