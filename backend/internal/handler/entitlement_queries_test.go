package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
)

func TestEntitlementQueriesAuthorityAndEffectiveState(t *testing.T) {
	f := newOrderFixture(t)
	order := f.paid(t, f.create(t, 0).ID)
	other := assignmentBuyer(t, f)
	ctx := context.Background()
	service := f.h.services.SubscriptionQueries
	page, err := service.Owned(ctx, other.ID, entitlements.SubscriptionQuery{UserID: 1})
	if err != nil || page.Total != 0 {
		t.Fatalf("owner override: %+v %v", page, err)
	}
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", order.SubscriptionID).Update("flow_used", 1024).Error; err != nil {
		t.Fatal(err)
	}
	detail, err := service.Detail(ctx, 1, order.SubscriptionID)
	if err != nil || detail.Status != "expired" || detail.PlanName == "" || detail.SKUName == "" {
		t.Fatalf("detail projection: %+v %v", detail, err)
	}
	var stored model.Subscription
	if err := f.h.db.First(&stored, order.SubscriptionID).Error; err != nil || stored.Status != "active" {
		t.Fatal("read unexpectedly mutated subscription")
	}
	page, err = service.Administrative(ctx, 1, entitlements.SubscriptionQuery{Status: "expired", Quota: "exhausted"})
	if err != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("effective filtering: %+v %v", page, err)
	}
	if _, err := service.Owned(ctx, 1, entitlements.SubscriptionQuery{Limit: 201}); err == nil {
		t.Fatal("oversize page accepted")
	}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Detail(ctx, 1, order.SubscriptionID); !errors.Is(err, entitlements.ErrAdministrativeRead) {
		t.Fatalf("revoked detail: %v", err)
	}
	if _, err := service.Administrative(ctx, 1, entitlements.SubscriptionQuery{}); !errors.Is(err, entitlements.ErrAdministrativeRead) {
		t.Fatalf("revoked list: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := service.Owned(canceled, 1, entitlements.SubscriptionQuery{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}
