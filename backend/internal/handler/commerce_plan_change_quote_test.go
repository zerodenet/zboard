package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
)

const quoteGB = int64(1024 * 1024 * 1024)

func preparePricedChange(t *testing.T, used int64, elapsedDays int) (orderFixture, model.Subscription, model.Order) {
	t.Helper()
	f := newOrderFixture(t)
	f.h.db.Model(&f.planRecord).Update("traffic_bytes", 100*quoteGB)
	f.h.db.Model(&f.skuRecord).Update("price_cents", 1000)
	root := f.paid(t, f.create(t, 0).ID)
	now := time.Now().UTC()
	start := now.Add(-time.Duration(elapsedDays) * 24 * time.Hour)
	end := start.Add(30 * 24 * time.Hour)
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", root.SubscriptionID).Updates(map[string]any{"start_at": start, "end_at": end, "flow_used": used}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&root).Update("paid_at", start).Error; err != nil {
		t.Fatal(err)
	}
	var sub model.Subscription
	f.h.db.First(&sub, root.SubscriptionID)
	plan := f.plan(t, 2)
	f.h.db.Model(&plan).Update("traffic_bytes", 150*quoteGB)
	sku := f.sku(t, plan.ID, 1500, "change")
	f.planRecord, f.skuRecord = plan, sku
	return f, sub, root
}
func previewPricedOrder(t *testing.T, f orderFixture, target uint) commerce.OrderPreview {
	t.Helper()
	w := httptest.NewRecorder()
	f.h.OrderPreviewHandler(w, announcementRequest(http.MethodPost, "/api/v1/orders/preview", f.token, fmt.Sprintf(`{"plan_sku_id":%d,"target_subscription_id":%d}`, f.skuRecord.ID, target)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct{ Data commerce.OrderPreview }
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}
func TestPlanChangeQuoteAndPaymentUseRemainingPaidValue(t *testing.T) {
	for _, tc := range []struct {
		name   string
		used   int64
		days   int
		credit int64
	}{
		{"unused", 0, 0, 1000}, {"half time", 0, 15, 500}, {"traffic tighter", 75 * quoteGB, 15, 250},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, before, _ := preparePricedChange(t, tc.used, tc.days)
			var count int64
			f.h.db.Model(&model.Order{}).Count(&count)
			quote := previewPricedOrder(t, f, before.ID)
			var afterCount int64
			f.h.db.Model(&model.Order{}).Count(&afterCount)
			if count != afterCount {
				t.Fatal("preview created an order")
			}
			if quote.CreditAmount != tc.credit || quote.PayableAmount != 1500-tc.credit || quote.TrafficBytes != 150*quoteGB || !quote.EndAt.Equal(before.EndAt) {
				t.Fatal(quote)
			}
			order, err := f.h.services.OrderCreation.Create(context.Background(), 1, commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, TargetSubscriptionID: before.ID, QuoteFingerprint: quote.QuoteFingerprint})
			if err != nil {
				t.Fatal(err)
			}
			if order.DiscountAmount != tc.credit || order.PayableAmount != quote.PayableAmount {
				t.Fatal(order)
			}
			paid := f.paid(t, order.ID)
			var after model.Subscription
			f.h.db.First(&after, before.ID)
			if paid.PaidAmount != quote.PayableAmount || after.FlowTotal != 150*quoteGB || after.FlowUsed != before.FlowUsed || !after.EndAt.Equal(before.EndAt) || after.PlanID != 2 {
				t.Fatal(paid, after)
			}
			// Repeated callback cannot grant quota or transferred credit again.
			f.paid(t, order.ID)
			var events int64
			f.h.db.Model(&model.QuotaEvent{}).Where("event_type = ? AND reference_id = ?", "plan_change", fmt.Sprint(order.ID)).Count(&events)
			if events != 1 {
				t.Fatal("duplicate plan-change grant", events)
			}
		})
	}
}
func TestPlanChangeRejectsChangedQuoteAndReusedSource(t *testing.T) {
	f, sub, _ := preparePricedChange(t, 0, 0)
	quote := previewPricedOrder(t, f, sub.ID)
	f.h.db.Model(&model.Subscription{}).Where("id = ?", sub.ID).Update("flow_used", 10*quoteGB)
	_, err := f.h.services.OrderCreation.Create(context.Background(), 1, commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, TargetSubscriptionID: sub.ID, QuoteFingerprint: quote.QuoteFingerprint})
	if err == nil {
		t.Fatal("stale quote accepted")
	}
	first := f.create(t, sub.ID)
	second := f.create(t, sub.ID)
	f.paid(t, first.ID)
	if response := f.pay(t, second.ID, false); response.Code != 400 {
		t.Fatal("spent source accepted", response.Code, response.Body.String())
	}
	var pending model.Order
	f.h.db.First(&pending, second.ID)
	if pending.Status != "pending" {
		t.Fatal("rejected settlement changed order", pending)
	}
}
func TestPlanChangeCarriesOnlyPaidAndTransferredValue(t *testing.T) {
	f, sub, _ := preparePricedChange(t, 40*quoteGB, 0)
	first := f.paid(t, f.create(t, sub.ID).ID)
	if first.DiscountAmount != 600 || first.PaidAmount != 900 {
		t.Fatal(first)
	}
	plan := f.plan(t, 3)
	f.h.db.Model(&plan).Update("traffic_bytes", 200*quoteGB)
	sku := f.sku(t, plan.ID, 2000, "change")
	f.planRecord, f.skuRecord = plan, sku
	quote := previewPricedOrder(t, f, sub.ID)
	// B's value is 900 paid + 600 transferred, not old A + full B again.
	if quote.TrafficCredit != 1100 || quote.CreditAmount != 1100 || quote.PayableAmount != 900 {
		t.Fatal(quote)
	}
}

func TestPlanChangeAdministratorOverrideTransfersOnlyAppliedCredit(t *testing.T) {
	f, sub, _ := preparePricedChange(t, 0, 0)
	quote := previewPricedOrder(t, f, sub.ID)
	amount := int64(1500)
	request := assignmentRequest(f, 1)
	request.TargetSubscriptionID = sub.ID
	request.PayableAmount = &amount
	request.QuoteFingerprint = quote.QuoteFingerprint
	response, order := assignOrder(t, f, request)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	paid := f.paid(t, order.ID)
	if paid.DiscountAmount != 0 || paid.PaidAmount != 1500 {
		t.Fatal(paid)
	}
	plan := f.plan(t, 3)
	f.h.db.Model(&plan).Update("traffic_bytes", 200*quoteGB)
	sku := f.sku(t, plan.ID, 2000, "change")
	f.planRecord, f.skuRecord = plan, sku
	next := previewPricedOrder(t, f, sub.ID)
	if next.CreditAmount != 1500 || next.PayableAmount != 500 {
		t.Fatal("unapplied old credit inflated value", next)
	}
}
