package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"testing"
)

func TestCommerceOrderCommandsRecheckAuthorityAndAtomicity(t *testing.T) {
	f := newOrderFixture(t)
	buyer := assignmentBuyer(t, f)
	ctx := context.Background()
	request := assignmentRequest(f, buyer.ID)
	order, err := f.h.services.OrderAssignment.Assign(ctx, 1, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.OrderCancellation.Owned(ctx, 1, order.ID); !errors.Is(err, commerce.ErrOrderPermission) {
		t.Fatalf("admin in user scope: %v", err)
	}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.OrderAssignment.Assign(ctx, 1, request); !errors.Is(err, commerce.ErrOrderPermission) {
		t.Fatalf("revoked assignment replay: %v", err)
	}
	if _, err := f.h.services.OrderCancellation.Administrative(ctx, 1, order.ID, true); !errors.Is(err, commerce.ErrOrderPermission) {
		t.Fatalf("revoked force cancel: %v", err)
	}
	const callback = "order-cancel-audit-failure"
	if err := f.h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_logs" {
			tx.AddError(errors.New("audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	out, err := f.h.services.OrderCancellation.Owned(ctx, buyer.ID, order.ID)
	f.h.db.Callback().Create().Remove(callback)
	if err == nil || out.ID != 0 {
		t.Fatalf("failed cancel returned order: %+v %v", out, err)
	}
	var stored model.Order
	if err := f.h.db.First(&stored, order.ID).Error; err != nil || stored.Status != "pending" || stored.CanceledAt != nil {
		t.Fatalf("cancel not rolled back: %+v %v", stored, err)
	}
	for i := 0; i < 2; i++ {
		out, err = f.h.services.OrderCancellation.Owned(ctx, buyer.ID, order.ID)
		if err != nil || out.Status != "canceled" || out.CanceledAt == nil {
			t.Fatalf("cancel/replay: %+v %v", out, err)
		}
	}
	var count int64
	if err := f.h.db.Model(&model.AuditLog{}).Where("action = ?", "order.cancel").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("cancel audit count=%d %v", count, err)
	}
}
