package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestDeliveryReviewFencesRetryAndCommitsAuditAtomically(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	actor := model.User{Email: "delivery-review@example.test", Password: "unused", IsAdmin: true, Status: "active"}
	if err := h.db.Create(&actor).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{Type: "email", Content: `{}`, Scope: `{}`, Status: 3, Total: 2, Attempts: 1, MaxAttempts: 3, IdempotencyKey: "delivery-review-fixture"}
	if err := h.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	items := []model.TaskItem{{TaskID: task.ID, TargetType: "user", TargetID: "1", Payload: `{}`, Status: 3, Attempts: 1, DeliveryState: "unknown"}, {TaskID: task.ID, TargetType: "user", TargetID: "2", Payload: `{}`, Status: 3, Attempts: 1, DeliveryState: "unknown"}}
	if err := h.db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.enqueueAdminTask(task.ID, nil); !errors.Is(err, messaging.ErrDeliveryReviewRequired) {
		t.Fatal("unknown mail requeued", err)
	}
	if _, err := h.claimTask(task.ID, nil); !errors.Is(err, messaging.ErrDeliveryReviewRequired) {
		t.Fatal("unknown mail claimed", err)
	}
	service := h.services.DeliveryReview()
	input := messaging.DeliveryReviewInput{ExpectedAttempt: 1, Acceptance: messaging.Accepted, Reason: "Provider log confirmed acceptance"}
	failure := errors.New("fixture review audit failure")
	const callback = "delivery-review-audit-failure"
	if err := h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_logs" {
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	err := service.Review(context.Background(), actor.ID, task.ID, items[0].ID, input)
	h.db.Callback().Create().Remove(callback)
	if !errors.Is(err, failure) {
		t.Fatal("audit failure not reached", err)
	}
	if err := h.db.First(&items[0], items[0].ID).Error; err != nil || items[0].DeliveryState != "unknown" {
		t.Fatal("review escaped rollback", items[0], err)
	}
	if err := service.Review(context.Background(), actor.ID, task.ID, items[0].ID, input); err != nil {
		t.Fatal(err)
	}
	if err := h.enqueueAdminTask(task.ID, nil); !errors.Is(err, messaging.ErrDeliveryReviewRequired) {
		t.Fatal("another unknown item bypassed", err)
	}
	input.ExpectedAttempt = 2
	input.Acceptance = messaging.NotAccepted
	if err := service.Review(context.Background(), actor.ID, task.ID, items[1].ID, input); !errors.Is(err, messaging.ErrDeliveryReviewConflict) {
		t.Fatal("stale attempt reviewed", err)
	}
	input.ExpectedAttempt = 1
	if err := h.db.Model(&actor).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Review(context.Background(), actor.ID, task.ID, items[1].ID, input); !errors.Is(err, messaging.ErrTemplatePermission) {
		t.Fatal("revoked admin reviewed", err)
	}
	if err := h.db.Model(&actor).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Review(context.Background(), actor.ID, task.ID, items[1].ID, input); err != nil {
		t.Fatal(err)
	}
	if err := h.enqueueAdminTask(task.ID, nil); err != nil {
		t.Fatal("reviewed mail cannot requeue", err)
	}
	if err := h.db.First(&items[0], items[0].ID).Error; err != nil || items[0].Status != 2 {
		t.Fatal("accepted recipient became retryable", items[0], err)
	}
	var audits int64
	if err := h.db.Model(&model.AuditLog{}).Where("action = ?", "message.delivery.review").Count(&audits).Error; err != nil || audits != 2 {
		t.Fatal(audits, err)
	}
}
