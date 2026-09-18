package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"testing"
)

func TestMessageRequestsOwnRecipientsSnapshotAndAudit(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	if err := h.db.Create(&model.Installation{ID: 1, SiteName: "Core site", SiteURL: "https://example.test"}).Error; err != nil {
		t.Fatal(err)
	}
	admin := model.User{Email: "message-admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
	inactive := model.User{Email: "message-inactive@example.test", Password: "unused", Status: "disabled"}
	for _, u := range []*model.User{&admin, &inactive} {
		if err := h.db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := h.services.MessageRequests()
	in := messaging.RequestInput{Scope: messaging.RecipientScope{UserIDs: []uint{admin.ID, admin.ID, inactive.ID}}, Content: messaging.EmailContent{Subject: "Hello", Body: "Welcome {{account_name}}", SiteName: "forged", TemplateRevision: 999}, IdempotencyKey: "message-request-test", AutoRun: true}
	receipt, err := service.Create(context.Background(), admin.ID, in)
	if err != nil || receipt.Total != 1 || receipt.ScheduledAt == nil || receipt.Status != 0 {
		t.Fatal(receipt, err)
	}
	var content messaging.EmailContent
	if err := json.Unmarshal([]byte(receipt.Content), &content); err != nil {
		t.Fatal(err)
	}
	if content.SiteName == "forged" || content.TemplateRevision != 0 {
		t.Fatal("untrusted provenance accepted", content)
	}
	var items []model.TaskItem
	if err := h.db.Where("task_id = ?", receipt.ID).Find(&items).Error; err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	if _, err := service.Create(context.Background(), admin.ID, in); !errors.Is(err, messaging.ErrRequestConflict) {
		t.Fatal("duplicate accepted", err)
	}
	failure := errors.New("message audit fixture failure")
	const callback = "message-request-audit-failure"
	if err := h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_logs" {
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	in.IdempotencyKey = "message-request-rollback"
	_, err = service.Create(context.Background(), admin.ID, in)
	h.db.Callback().Create().Remove(callback)
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	var count int64
	if err := h.db.Model(&model.Task{}).Where("idempotency_key = ?", in.IdempotencyKey).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("task escaped rollback", count, err)
	}
	if err := h.db.Model(&admin).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(context.Background(), admin.ID, in); !errors.Is(err, messaging.ErrTemplatePermission) {
		t.Fatal("revoked administrator created request", err)
	}
}
