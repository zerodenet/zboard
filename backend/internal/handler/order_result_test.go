package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestRepeatedSettlementDoesNotRegrantOrScheduleConfig(t *testing.T) {
	f := newOrderFixture(t)
	first := f.paid(t, f.create(t, 0).ID)
	var before model.Subscription
	if err := f.h.db.First(&before, first.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	endpointReads := 0
	if err := f.h.db.Callback().Query().After("gorm:query").Register("test:count_settlement_endpoint_reads", func(tx *gorm.DB) {
		if tx.Statement.Table == "protocol_endpoints" {
			endpointReads++
		}
	}); err != nil {
		t.Fatal(err)
	}
	for _, callback := range []bool{false, true, true} {
		if w := f.pay(t, first.ID, callback); w.Code != http.StatusOK {
			t.Fatalf("repeat: %d %s", w.Code, w.Body.String())
		}
	}
	if endpointReads != 0 {
		t.Fatalf("idempotent confirmation still queried endpoint delivery %d times", endpointReads)
	}
	var after model.Subscription
	if err := f.h.db.First(&after, first.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if after.FlowTotal != before.FlowTotal || !after.EndAt.Equal(before.EndAt) {
		t.Fatal("repeated settlement granted entitlement again")
	}
	var actual model.Order
	if err := f.h.db.First(&actual, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if actual.PaidAt == nil || !actual.PaidAt.Equal(*first.PaidAt) || actual.FulfilledAt == nil || !actual.FulfilledAt.Equal(*first.FulfilledAt) {
		t.Fatal("idempotent result replaced settlement timestamps")
	}
	for _, table := range []string{"subscriptions", "quota_events", "audit_logs"} {
		var count int64
		if err := f.h.db.Table(table).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
}

func TestPaidOrderRejectsLaterFailureOrCancellation(t *testing.T) {
	f := newOrderFixture(t)
	order := f.paid(t, f.create(t, 0).ID)
	for _, status := range []string{orderStatusFailed, orderStatusCanceled} {
		w := httptest.NewRecorder()
		f.h.OrderPayCallbackCommerceHandler(w, announcementRequest(http.MethodPost,
			fmt.Sprintf("/api/v1/orders/%d/pay-callback", order.ID), f.token, fmt.Sprintf(`{"status":%q}`, status)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("paid -> %s: %d %s", status, w.Code, w.Body.String())
		}
	}
	var actual model.Order
	if err := f.h.db.First(&actual, order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if actual.Status != orderStatusPaid || actual.CanceledAt != nil {
		t.Fatal("late result rolled paid order back")
	}
}

func TestOrderResultHonorsCanceledContextWithoutMutation(t *testing.T) {
	f := newOrderFixture(t)
	order := f.create(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := f.h.applyOrderResult(ctx, orderResultCommand{OrderID: order.ID, Status: orderStatusPaid, Actor: authClaims{UserID: 1}})
	if !errors.Is(err, context.Canceled) || result.Fulfilled || result.Order.ID != 0 {
		t.Fatalf("canceled result = %+v, %v", result, err)
	}
	var actual model.Order
	if err := f.h.db.First(&actual, order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if actual.Status != orderStatusPending || actual.PaidAt != nil {
		t.Fatal("canceled command committed settlement")
	}
}
