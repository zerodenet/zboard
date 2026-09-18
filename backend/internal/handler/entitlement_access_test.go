package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"testing"
)

func TestEntitlementAccessLifecycleAndAuthority(t *testing.T) {
	f := newOrderFixture(t)
	order := f.paid(t, f.create(t, 0).ID)
	ctx := context.Background()
	service := f.h.services.SubscriptionAccess(f.h.credentialCipher)
	first, err := service.Read(ctx, 1, order.SubscriptionID)
	if err != nil || !first.Configured || first.Token == "" {
		t.Fatalf("read: %+v %v", first.Configured, err)
	}
	other := assignmentBuyer(t, f)
	if _, err := service.Read(ctx, other.ID, order.SubscriptionID); !errors.Is(err, entitlements.ErrAccessNotFound) {
		t.Fatalf("other owner: %v", err)
	}
	const callback = "access-audit-rollback"
	if err := f.h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_logs" {
			tx.AddError(errors.New("audit unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	failed, err := service.Rotate(ctx, 1, order.SubscriptionID)
	f.h.db.Callback().Create().Remove(callback)
	if err == nil || failed.Token != "" {
		t.Fatal("failed rotation returned credential")
	}
	again, err := service.Read(ctx, 1, order.SubscriptionID)
	if err != nil || again.Token != first.Token {
		t.Fatal("failed rotation changed token")
	}
	rotated, err := service.Rotate(ctx, 1, order.SubscriptionID)
	if err != nil || rotated.Token == first.Token || rotated.Token == "" {
		t.Fatal("rotation failed")
	}
	revoked, err := service.Revoke(ctx, 1, order.SubscriptionID)
	if err != nil || !revoked.Revoked {
		t.Fatalf("revoke: %v", err)
	}
	after, err := service.Read(ctx, 1, order.SubscriptionID)
	if err != nil || after.Configured || after.Token != "" {
		t.Fatal("read reactivated revoked token")
	}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("status", "disabled").Error; err != nil {
		t.Fatal(err)
	}
	for _, operation := range []func(context.Context, uint, uint) (entitlements.AccessView, error){service.Read, service.Rotate, service.Revoke} {
		out, err := operation(ctx, 1, order.SubscriptionID)
		if !errors.Is(err, entitlements.ErrAccessPermission) || out.Token != "" {
			t.Fatalf("disabled account: %v", err)
		}
	}
}
