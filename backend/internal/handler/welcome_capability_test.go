package handler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestWelcomeIntentIsIdempotentAndAuditAtomic(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	ctx := context.Background()
	var user model.User
	if err := h.db.First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Create(&model.Installation{ID: 1, SiteName: "Snapshot", SiteURL: "https://site.example", InstalledAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	for _, config := range []model.SystemConfig{
		{ConfigKey: "task_email_enabled", Value: "true", ValueType: "bool"},
		{ConfigKey: "smtp_host", Value: "smtp.example.test", ValueType: "string"},
		{ConfigKey: "smtp_port", Value: "587", ValueType: "int"},
		{ConfigKey: "smtp_from", Value: "sender@example.test", ValueType: "string"},
		{ConfigKey: "smtp_tls_mode", Value: "starttls", ValueType: "string"},
	} {
		if err := h.db.Create(&config).Error; err != nil {
			t.Fatal(err)
		}
	}
	trigger := "user.registered"
	template := model.EmailTemplate{Name: "Welcome", Slug: "welcome", Category: "registration", TriggerKey: &trigger, SubjectTemplate: "Hello", BodyTemplate: "Original", IsActive: true, Revision: 1}
	if err := h.db.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	service := h.services.RegistrationWelcome(h.credentialCipher)
	first, err := service.Enqueue(ctx, user.ID)
	if err != nil || first.TaskID == 0 || !first.Created {
		t.Fatal(first, err)
	}
	if err := h.db.Model(&template).Update("body_template", "Changed").Error; err != nil {
		t.Fatal(err)
	}
	again, err := service.Enqueue(ctx, user.ID)
	if err != nil || again.TaskID != first.TaskID || again.Created {
		t.Fatal(again, err)
	}
	var task model.Task
	if err := h.db.First(&task, first.TaskID).Error; err != nil {
		t.Fatal(err)
	}
	var content messaging.EmailContent
	if err := json.Unmarshal([]byte(task.Content), &content); err != nil {
		t.Fatal(err)
	}
	if content.Body != "Original" || content.SiteName != "Snapshot" || task.ScheduledAt == nil || task.Type != "email" {
		t.Fatal("snapshot or scheduled intent lost", task)
	}
	var items, audits int64
	if err := h.db.Model(&model.TaskItem{}).Where("task_id = ?", task.ID).Count(&items).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&model.AuditLog{}).Where("action = ?", "notification.enqueue").Count(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if items != 1 || audits != 1 {
		t.Fatal(items, audits)
	}
	if err := h.db.Model(&template).Update("revision", 2).Error; err != nil {
		t.Fatal(err)
	}
	failure := errors.New("fixture audit failure")
	const callback = "welcome-audit-failure"
	if err := h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_logs" {
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, err = service.Enqueue(ctx, user.ID)
	h.db.Callback().Create().Remove(callback)
	if !errors.Is(err, failure) {
		t.Fatal("audit failure not reached", err)
	}
	var tasks int64
	if err := h.db.Model(&model.Task{}).Count(&tasks).Error; err != nil || tasks != 1 {
		t.Fatal("failed intent escaped rollback", tasks, err)
	}
	if err := h.db.Model(&user).Update("status", "suspended").Error; err != nil {
		t.Fatal(err)
	}
	skipped, err := service.Enqueue(ctx, user.ID)
	if err != nil || skipped.TaskID != 0 {
		t.Fatal("suspended recipient enqueued", skipped, err)
	}
	if err := h.db.Model(&user).Update("status", "active").Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Create(&model.AccountRegistrationEvent{AccountID: user.ID, OccurredAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	const checkpointCallback = "welcome-checkpoint-failure"
	if err := h.db.Callback().Update().Before("gorm:update").Register(checkpointCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == "account_registration_events" {
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	events := h.services.RegistrationMessages(h.credentialCipher)
	_, err = events.Process(ctx)
	h.db.Callback().Update().Remove(checkpointCallback)
	if !errors.Is(err, failure) {
		t.Fatal("checkpoint failure not reached", err)
	}
	if err := h.db.Model(&model.Task{}).Count(&tasks).Error; err != nil || tasks != 1 {
		t.Fatal("task escaped checkpoint rollback", tasks, err)
	}
	processed, err := events.Process(ctx)
	if err != nil || processed != 1 {
		t.Fatal(processed, err)
	}
	var event model.AccountRegistrationEvent
	if err := h.db.First(&event, "account_id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if event.ProcessedAt == nil || event.TaskID == nil {
		t.Fatal("event not associated with committed task", event)
	}
	processed, err = events.Process(ctx)
	if err != nil || processed != 0 {
		t.Fatal("completed event replayed", processed, err)
	}
}
