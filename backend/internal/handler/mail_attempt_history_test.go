package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func TestMailAttemptsPreserveRetryEvidenceAndFenceCompletion(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	until := time.Now().UTC().Add(time.Minute)
	task := model.Task{Type: "email", Content: `{}`, Scope: `{}`, Status: 1, LockedBy: "history-owner", LockedUntil: &until, Total: 1, MaxAttempts: 3, IdempotencyKey: "mail-history-fixture"}
	if err := h.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	// Missing recipient fails before SMTP but still exercises the real task path.
	item := model.TaskItem{TaskID: task.ID, TargetType: "user", TargetID: "999999999", Payload: `{}`}
	if err := h.db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if failures := h.executeClaimedTaskItem(task, &item, task.LockedBy); len(failures) == 0 {
			t.Fatal("missing recipient unexpectedly succeeded")
		}
	}
	var rows []model.MailDeliveryAttempt
	if err := h.db.Where("task_id = ?", task.ID).Order("attempt").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("lost history: %+v", rows)
	}
	for i, row := range rows {
		if row.Attempt != i+1 || row.Acceptance != string(messaging.NotAccepted) || row.FinishedAt == nil {
			t.Fatalf("bad evidence: %+v", row)
		}
	}
	failure := errors.New("fixture rollback")
	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&item).Updates(map[string]any{"status": 1, "attempts": 3}).Error; err != nil {
			return err
		}
		if _, err := application.StartMailAttempt(tx, task.ID, item.ID, task.LockedBy, time.Now().UTC()); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	var count int64
	h.db.Model(&model.MailDeliveryAttempt{}).Where("task_id = ?", task.ID).Count(&count)
	if count != 2 {
		t.Fatal("history escaped rollback", count)
	}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&item).Updates(map[string]any{"status": 1, "attempts": 3}).Error; err != nil {
			return err
		}
		_, err := application.StartMailAttempt(tx, task.ID, item.ID, task.LockedBy, time.Now().UTC())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		return application.FinishMailAttempt(tx, task.ID, item.ID, 3, "stale-owner", messaging.Accepted, time.Now().UTC())
	})
	if !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal("stale completion accepted", err)
	}
	var pending model.MailDeliveryAttempt
	if err := h.db.Where("item_id = ? AND attempt = 3", item.ID).First(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if pending.Acceptance != "unknown" || pending.FinishedAt != nil {
		t.Fatal("uncertainty lost", pending)
	}
}

func TestMailHistoryScopesPagesAndChecksCurrentAdministrator(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	admin := model.User{Email: "history-admin@example.test", Password: "unused", IsAdmin: true, Status: "active"}
	if err := h.db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{Type: "email", Content: `{}`, Scope: `{}`, IdempotencyKey: "history-query-fixture"}
	if err := h.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	item := model.TaskItem{TaskID: task.ID, TargetType: "user", TargetID: "1", Payload: `{}`}
	if err := h.db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		if err := h.db.Create(&model.MailDeliveryAttempt{TaskID: task.ID, ItemID: item.ID, Attempt: i, LeaseToken: "private-lease-token", Acceptance: "unknown", StartedAt: time.Now().UTC()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := h.services.DeliveryHistory()
	page, err := service.List(context.Background(), admin.ID, task.ID, item.ID, 1, 1)
	if err != nil || page.Total != 3 || len(page.Items) != 1 || page.Items[0].Attempt != 2 {
		t.Fatal(page, err)
	}
	data, _ := json.Marshal(page)
	if strings.Contains(string(data), "private-lease-token") || strings.Contains(string(data), "lease") {
		t.Fatal("execution authority leaked")
	}
	if _, err := service.List(context.Background(), admin.ID, task.ID+1, item.ID, 25, 0); !errors.Is(err, messaging.ErrDeliveryNotFound) {
		t.Fatal("foreign item read", err)
	}
	if err := h.db.Model(&admin).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	page, err = service.List(context.Background(), admin.ID, task.ID, item.ID, 25, 0)
	if !errors.Is(err, messaging.ErrTemplatePermission) || len(page.Items) != 0 {
		t.Fatal("revoked administrator read history", page, err)
	}
}
