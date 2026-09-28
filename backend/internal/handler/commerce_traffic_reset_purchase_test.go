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

func prepareResetSKU(t *testing.T, f orderFixture) orderFixture {
	t.Helper()
	w := httptest.NewRecorder()
	f.h.PlanSKUCreateCommerceHandler(w, announcementRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/plans/%d/skus", f.planRecord.ID), f.token, `{"code":"reset-month","name":"重置流量","billing_mode":"one_time","entitlement_mode":"traffic_reset","allowed_operations":["reset"],"billing_unit":"once","billing_value":1,"price_cents":500,"currency":"CNY","is_active":true}`))
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct{ Data commerce.SKUView }
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	var sku model.PlanSKU
	if err := f.h.db.First(&sku, response.Data.ID).Error; err != nil {
		t.Fatal(err)
	}
	var operations []model.PlanSKUOperation
	if err := f.h.db.Where("plan_sku_id = ?", sku.ID).Find(&operations).Error; err != nil || len(operations) != 1 || operations[0].Operation != "reset" {
		t.Fatal(operations, err)
	}
	f.skuRecord = sku
	return f
}
func TestPurchasedTrafficResetRestoresExhaustedTimedSubscription(t *testing.T) {
	f := newOrderFixture(t)
	endpoint := attachOrderPublishEndpoint(t, f)
	f.h.db.Model(&f.planRecord).Updates(map[string]any{"traffic_bytes": 100 * quoteGB, "reset_policy": 2})
	paid := f.paid(t, f.create(t, 0).ID)
	var before model.Subscription
	f.h.db.First(&before, paid.SubscriptionID)
	if err := f.h.db.Model(&before).Updates(map[string]any{"flow_used": 100 * quoteGB, "status": "expired"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := expireSubscriptions(f.h.db, 1, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	f = prepareResetSKU(t, f)
	var candidates struct {
		Items []adminSubscriptionListItem
		Total int
	}
	if status := f.get(t, "/api/v1/subscriptions?paged=true&eligible_for=reset", f.h.SubscriptionsHandler, &candidates); status != 200 || candidates.Total != 1 {
		t.Fatal(status, candidates)
	}
	// Public catalog and SKU filtering use the same reset operation registry.
	if status := f.get(t, fmt.Sprintf("/api/v1/plans?operation=reset&plan_id=%d", f.planRecord.ID), f.h.PlanListCommerceHandler, nil); status != 200 {
		t.Fatal(status)
	}
	quote := previewPricedOrder(t, f, before.ID)
	if quote.OrderType != "traffic_reset" || quote.TrafficBytes != 100*quoteGB || quote.PayableAmount != 500 || quote.CreditAmount != 0 {
		t.Fatal(quote)
	}
	order := f.create(t, before.ID)
	var pending model.Subscription
	f.h.db.First(&pending, before.ID)
	if pending.FlowUsed != pending.FlowTotal || pending.CycleStartUsed != before.CycleStartUsed {
		t.Fatal("unpaid reset restored quota", pending)
	}
	// An ordinary customer cannot report a paid result and grant their own reset.
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if response := f.pay(t, order.ID, true); response.Code != http.StatusForbidden {
		t.Fatal("customer callback granted a reset", response.Code, response.Body.String())
	}
	f.h.db.First(&pending, before.ID)
	if pending.FlowUsed != pending.FlowTotal {
		t.Fatal("denied callback changed quota", pending)
	}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	// Later plan edits cannot change the target's purchased base quota.
	f.h.db.Model(&f.planRecord).Update("traffic_bytes", 150*quoteGB)
	f.paid(t, order.ID)
	var after model.Subscription
	f.h.db.First(&after, before.ID)
	total, used := subscriptionCycleQuota(after)
	if total != 100*quoteGB || used != 0 || after.FlowUsed != 100*quoteGB || after.FlowTotal != 200*quoteGB || after.Status != "active" || after.PlanSKUID != before.PlanSKUID || !after.EndAt.Equal(before.EndAt) || !after.NextResetAt.Equal(*before.NextResetAt) {
		t.Fatal(after)
	}
	var credential model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ? AND protocol_endpoint_id = ? AND status = ?", before.ID, endpoint.ID, "active").First(&credential).Error; err != nil {
		t.Fatal("reset did not restore credentials", err)
	}
	var event model.QuotaEvent
	f.h.db.Where("event_type = ? AND reference_id = ?", "traffic_reset", fmt.Sprint(order.ID)).First(&event)
	if event.DeltaBytes != 100*quoteGB || event.BalanceAfter != 100*quoteGB {
		t.Fatal(event)
	}
	f.paid(t, order.ID)
	f.h.db.First(&after, before.ID)
	if after.FlowTotal != 200*quoteGB {
		t.Fatal("repeated payment reset quota again", after)
	}
	// Next calendar reset does not replay the paid reset as additional quota.
	future := *before.NextResetAt
	f.h.db.Model(&after).Update("end_at", future.Add(24*time.Hour))
	if err := f.h.runDueTrafficResets(future.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	f.h.db.First(&after, before.ID)
	if total, used := subscriptionCycleQuota(after); total != 100*quoteGB || used != 0 {
		t.Fatal("paid reset repeated at scheduled reset", after)
	}
}

func TestCanceledTrafficResetNeverRestoresQuota(t *testing.T) {
	f := newOrderFixture(t)
	paid := f.paid(t, f.create(t, 0).ID)
	f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Update("flow_used", 1024)
	f = prepareResetSKU(t, f)
	order := f.create(t, paid.SubscriptionID)
	w := httptest.NewRecorder()
	f.h.OrderPayCallbackCommerceHandler(w, announcementRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%d/pay-callback", order.ID), f.token, `{"status":"canceled"}`))
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	if response := f.pay(t, order.ID, true); response.Code != http.StatusBadRequest {
		t.Fatal("canceled reset paid without explicit recovery", response.Code, response.Body.String())
	}
	var sub model.Subscription
	f.h.db.First(&sub, paid.SubscriptionID)
	if sub.FlowTotal != 1024 || sub.FlowUsed != 1024 || sub.CycleStartUsed != 0 {
		t.Fatal("failed payment restored exhausted quota", sub)
	}
	var events int64
	f.h.db.Model(&model.QuotaEvent{}).Where("event_type = ? AND reference_id = ?", "traffic_reset", fmt.Sprint(order.ID)).Count(&events)
	if events != 0 {
		t.Fatal("failed payment created a reset ledger event", events)
	}
}
func TestPurchasedTrafficResetReplacesLeftoverQuotaAndInvalidatesOlderReset(t *testing.T) {
	f := newOrderFixture(t)
	paid := f.paid(t, f.create(t, 0).ID)
	f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Update("flow_used", 400)
	f = prepareResetSKU(t, f)
	first := f.create(t, paid.SubscriptionID)
	older := f.create(t, paid.SubscriptionID)
	f.paid(t, first.ID)
	var sub model.Subscription
	f.h.db.First(&sub, paid.SubscriptionID)
	if sub.FlowTotal != 1424 || sub.FlowUsed != 400 || sub.CycleStartUsed != 400 {
		t.Fatal("reset stacked unused traffic", sub)
	}
	if w := f.pay(t, older.ID, false); w.Code != http.StatusBadRequest {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestPurchasedTrafficResetQuoteAllowsUsageButNotRepeatedReset(t *testing.T) {
	f := newOrderFixture(t)
	paid := f.paid(t, f.create(t, 0).ID)
	f = prepareResetSKU(t, f)
	quote := previewPricedOrder(t, f, paid.SubscriptionID)
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Update("flow_used", 400).Error; err != nil {
		t.Fatal(err)
	}
	created, err := f.h.services.OrderCreation.Create(context.Background(), 1, commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, TargetSubscriptionID: paid.SubscriptionID, QuoteFingerprint: quote.QuoteFingerprint})
	if err != nil {
		t.Fatal("usage invalidated fixed-price reset", err)
	}
	f.paid(t, created.ID)
	var sub model.Subscription
	f.h.db.First(&sub, paid.SubscriptionID)
	if sub.FlowTotal-sub.FlowUsed != 1024 {
		t.Fatal(sub)
	}
	// Even a reset at zero cycle usage advances the entitlement ledger version.
	first := f.create(t, sub.ID)
	second := f.create(t, sub.ID)
	f.paid(t, first.ID)
	if response := f.pay(t, second.ID, false); response.Code != 400 {
		t.Fatal("old no-op reset paid twice", response.Code, response.Body.String())
	}
}
