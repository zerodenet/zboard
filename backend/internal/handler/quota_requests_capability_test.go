package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"strconv"
	"testing"
	"time"
)

func TestQuotaRequestsScopeAndAuditAreAtomic(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	admin := model.User{Email: "quota-request-admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
	if err := h.db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	subs := []model.Subscription{{UserID: admin.ID, EndAt: time.Now().UTC().Add(time.Hour), Status: "active"}, {UserID: admin.ID, EndAt: time.Now().UTC().Add(-time.Hour), Status: "expired"}, {UserID: admin.ID + 1, EndAt: time.Now().UTC().Add(time.Hour), Status: "active"}}
	for i := range subs {
		if err := h.db.Create(&subs[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := h.services.QuotaRequests()
	in := entitlements.QuotaRequestInput{Scope: entitlements.QuotaScope{UserIDs: []uint{admin.ID}, SubscriptionIDs: []uint{subs[0].ID, subs[2].ID, subs[2].ID}}, Content: entitlements.QuotaAdjustment{DeltaMB: 1024, Reason: "manual adjustment"}, IdempotencyKey: "quota-request-fixture"}
	out, err := service.Create(context.Background(), admin.ID, in)
	if err != nil || out.Total != 2 || out.Type != "quota" || out.ScheduledAt != nil {
		t.Fatal(out, err)
	}
	var items []model.TaskItem
	if err := h.db.Where("task_id = ?", out.ID).Order("id").Find(&items).Error; err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].TargetID != strconv.FormatUint(uint64(subs[0].ID), 10) || items[1].TargetID != strconv.FormatUint(uint64(subs[2].ID), 10) {
		t.Fatal(items)
	}
	if _, err := service.Create(context.Background(), admin.ID, in); !errors.Is(err, entitlements.ErrQuotaRequestConflict) {
		t.Fatal("duplicate accepted", err)
	}
	const callback = "quota-request-audit-failure"
	failure := errors.New("fixture quota audit failure")
	if err := h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_logs" {
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	in.IdempotencyKey = "quota-request-rollback"
	in.AutoRun = true
	_, err = service.Create(context.Background(), admin.ID, in)
	h.db.Callback().Create().Remove(callback)
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	var count int64
	if err := h.db.Model(&model.Task{}).Where("idempotency_key = ?", in.IdempotencyKey).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("partial task persisted", count, err)
	}
	if err := h.db.Model(&admin).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(context.Background(), admin.ID, in); !errors.Is(err, entitlements.ErrAdministrativeRead) {
		t.Fatal("revoked administrator created batch", err)
	}
}
