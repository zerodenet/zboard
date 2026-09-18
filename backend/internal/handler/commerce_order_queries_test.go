package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"strings"
	"testing"
	"time"
)

func TestCommerceOrderQueriesScopePaginationAndDiagnostics(t *testing.T) {
	f := newOrderFixture(t)
	own := f.create(t, 0)
	buyer := assignmentBuyer(t, f)
	other, err := f.h.services.OrderAssignment.Assign(context.Background(), 1, assignmentRequest(f, buyer.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.Order{}).Where("id = ?", other.ID).Updates(map[string]interface{}{"status": "failed", "raw_callback": "private-callback-marker", "failure_reason": "private-failure-marker", "provider_trade_no": "provider-search"}).Error; err != nil {
		t.Fatal(err)
	}
	service := f.h.services.OrderQueries
	ctx := context.Background()
	page, err := service.Owned(ctx, 1, commerce.OrderQuery{UserID: buyer.ID, Search: "unmatched", OrderType: "invalid", From: time.Now()})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != own.ID {
		t.Fatalf("owner scope: %+v %v", page, err)
	}
	page, err = service.Administrative(ctx, 1, commerce.OrderQuery{Status: "attention", Limit: 1})
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != other.ID {
		t.Fatalf("page: %+v %v", page, err)
	}
	raw, _ := json.Marshal(page.Items)
	if strings.Contains(string(raw), "private-") {
		t.Fatal("list exposed diagnostics")
	}
	page, err = service.Administrative(ctx, 1, commerce.OrderQuery{Search: "provider-search"})
	if err != nil || page.Total != 1 || page.Items[0].ID != other.ID {
		t.Fatalf("provider search: %+v %v", page, err)
	}
	detail, err := service.Detail(ctx, 1, other.ID)
	if err != nil || detail.FailureReason != "private-failure-marker" {
		t.Fatalf("detail: %+v %v", detail, err)
	}
	raw, _ = json.Marshal(detail)
	if strings.Contains(string(raw), "private-callback-marker") {
		t.Fatal("detail exposed callback")
	}
	event := model.PaymentEvent{OrderID: other.ID, Provider: "test", ProviderEventID: "query-event", EventType: "failed", Payload: `{"secret":"private-event-marker"}`}
	if err := f.h.db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	events, err := service.Events(ctx, 1, other.ID, 0, 1)
	if err != nil || events.Total != 1 || len(events.Items) != 1 {
		t.Fatalf("events: %+v %v", events, err)
	}
	raw, _ = json.Marshal(events.Items)
	if strings.Contains(string(raw), "private-event-marker") {
		t.Fatal("event payload exposed")
	}
	if _, err := service.Administrative(ctx, 1, commerce.OrderQuery{Limit: 201}); err == nil {
		t.Fatal("unbounded page accepted")
	}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Administrative(ctx, 1, commerce.OrderQuery{}); !errors.Is(err, commerce.ErrOrderPermission) {
		t.Fatalf("revoked list: %v", err)
	}
	if _, err := service.Detail(ctx, 1, other.ID); !errors.Is(err, commerce.ErrOrderPermission) {
		t.Fatalf("revoked detail: %v", err)
	}
	if _, err := service.Events(ctx, 1, other.ID, 0, 1); !errors.Is(err, commerce.ErrOrderPermission) {
		t.Fatalf("revoked events: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := service.Owned(canceled, 1, commerce.OrderQuery{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
