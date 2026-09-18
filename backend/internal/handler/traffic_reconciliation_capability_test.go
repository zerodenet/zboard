package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
)

func TestTrafficReconciliationCapabilityScopesAndRevocation(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedReconciliation(t)
	service := f.h.services.Reconciliation()
	q := metering.ReconciliationQuery{UserID: 2}
	result, err := service.Read(context.Background(), 1, q)
	if err != nil || len(result.Items) != 4 {
		t.Fatalf("own scope: %+v %v", result, err)
	}
	for _, item := range result.Items {
		if item.UserID != 1 {
			t.Fatalf("foreign item: %+v", item)
		}
	}
	q.SubscriptionID = 5
	result, err = service.Read(context.Background(), 1, q)
	if err != nil || len(result.Items) != 0 {
		t.Fatalf("foreign subscription: %+v %v", result, err)
	}
	q = metering.ReconciliationQuery{Administrative: true}
	if _, err = service.Read(context.Background(), 99, q); err != nil {
		t.Fatal(err)
	}
	if err = f.h.db.Model(&model.User{}).Where("id = 99").Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = service.Read(context.Background(), 99, q); !errors.Is(err, metering.ErrTrendPermission) {
		t.Fatalf("revoked admin: %v", err)
	}
	if err = f.h.db.Model(&model.User{}).Where("id = 1").Update("status", "disabled").Error; err != nil {
		t.Fatal(err)
	}
	if _, err = service.Read(context.Background(), 1, metering.ReconciliationQuery{}); !errors.Is(err, metering.ErrTrendPermission) {
		t.Fatalf("disabled user: %v", err)
	}
}
