package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestCleanedSingleUseSubscriptionKeepsAccountOrderHistoryAndCannotBeRecreatedByLatePayment(t *testing.T) {
	f := newOrderFixture(t)
	if err := f.h.db.Model(&model.Plan{}).Where("id = ?", f.planRecord.ID).Update("is_renewable", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Where("plan_sku_id = ? AND operation = 'renew'", f.skuRecord.ID).Delete(&model.PlanSKUOperation{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.PlanSKU{}).Where("id = ?", f.skuRecord.ID).Updates(map[string]any{"billing_mode": "one_time", "billing_unit": "once", "renewal_effect": "none"}).Error; err != nil {
		t.Fatal(err)
	}
	paid := f.paid(t, f.create(t, 0).ID)
	var sub model.Subscription
	if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil || sub.Lifecycle != "fixed" || !sub.EndsOnQuotaExhaustion {
		t.Fatal(sub, err)
	}
	pending := model.Order{UserID: 1, PlanID: f.planRecord.ID, PlanSKUID: f.skuRecord.ID, TargetSubscriptionID: &sub.ID, TradeNo: "historical-pending-renewal", OrderType: "renewal", Status: "pending", BillingUnit: "once", BillingValue: 1}
	if err := f.h.db.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&sub).Update("flow_used", sub.FlowTotal).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.CredentialExpiry().ExpireDue(context.Background(), time.Now().UTC(), 10); err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []adminSubscriptionListItem
		Total int
	}
	if code := f.get(t, "/api/v1/subscriptions?paged=true", f.h.SubscriptionsHandler, &page); code != 200 || page.Total != 0 {
		t.Fatal("instance retained", code, page)
	}
	var orders struct {
		Items []adminOrderListItem
		Total int
	}
	if code := f.get(t, "/api/v1/orders?paged=true", f.h.OrderListHandler, &orders); code != 200 || orders.Total != 2 {
		t.Fatal("order history unavailable", code, orders)
	}
	found := false
	for _, item := range orders.Items {
		if item.ID == paid.ID {
			found = item.SubscriptionEndedAt != nil && item.SubscriptionEndReason == "exhausted" && item.SubscriptionFinalFlowUsed == 1024 && item.Status == "paid"
		}
	}
	if !found {
		t.Fatal("terminal history missing", orders)
	}
	w := httptest.NewRecorder()
	f.h.AdminOrderSubscriptionAccessHandler(w, announcementRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/orders/%d/subscription-access", paid.ID), f.token, ""))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "订单历史") {
		t.Fatal("deleted-instance access error", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	f.h.OrderPayCommerceHandler(w, announcementRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/orders/%d/pay?force=true", pending.ID), f.token, ""))
	if w.Code != http.StatusBadRequest {
		t.Fatal("late forced payment not rejected cleanly", w.Code, w.Body.String())
	}
	var count int64
	f.h.db.Model(&model.Subscription{}).Count(&count)
	if count != 0 {
		t.Fatal("late payment created a replacement instance")
	}
}

func TestLegacyExhaustedFixedServiceDisappearsBeforeCleanupAndHealthyServiceRemains(t *testing.T) {
	checkLegacyFixedRetirement(t, newOrderFixture(t))
}
func TestMySQLLegacyExhaustedFixedServiceDisappearsBeforeCleanupAndHealthyServiceRemains(t *testing.T) {
	checkLegacyFixedRetirement(t, newMySQLOrderFixture(t))
}
func checkLegacyFixedRetirement(t *testing.T, f orderFixture) {
	t.Helper()
	healthy := f.paid(t, f.create(t, 0).ID)
	if err := f.h.db.Model(&model.Plan{}).Where("id = ?", f.planRecord.ID).Update("is_renewable", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Where("plan_sku_id = ? AND operation = 'renew'", f.skuRecord.ID).Delete(&model.PlanSKUOperation{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.PlanSKU{}).Where("id = ?", f.skuRecord.ID).Updates(map[string]any{"billing_mode": "one_time", "billing_unit": "once", "renewal_effect": "none"}).Error; err != nil {
		t.Fatal(err)
	}
	fixed := f.paid(t, f.create(t, 0).ID)
	// Publishing a renewal SKU later must not rewrite this instance's snapshot.
	if err := f.h.db.Model(&model.Plan{}).Where("id = ?", f.planRecord.ID).Update("is_renewable", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.PlanSKU{}).Where("id = ?", f.skuRecord.ID).Updates(map[string]any{"billing_mode": "periodic", "billing_unit": "month", "renewal_effect": "extend_only"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.PlanSKUOperation{PlanSKUID: f.skuRecord.ID, Operation: "renew"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", fixed.SubscriptionID).Updates(map[string]any{"ends_on_quota_exhaustion": false, "flow_used": 1024, "end_at": time.Now().UTC().Add(365 * 24 * time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Exec("DELETE FROM schema_migrations WHERE version = '0026_fixed_quota_lifecycle.up.sql'").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"paged=true", "paged=true&status=active", "paged=true&eligible_for=manage", "paged=true&limit=1"} {
		var page struct {
			Items []adminSubscriptionListItem
			Total int
		}
		if code := f.get(t, "/api/v1/subscriptions?"+query, f.h.SubscriptionsHandler, &page); code != 200 || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != healthy.SubscriptionID {
			t.Fatal("wrong current service/count", query, code, page)
		}
	}
	var removed struct {
		Items []adminSubscriptionListItem
		Total int
	}
	if code := f.get(t, fmt.Sprintf("/api/v1/subscriptions?paged=true&subscription_id=%d&status=active", fixed.SubscriptionID), f.h.SubscriptionsHandler, &removed); code != 200 || removed.Total != 0 {
		t.Fatal("terminal target selected", code, removed)
	}
	if _, err := f.h.services.CredentialExpiry().ExpireDue(context.Background(), time.Now().UTC(), 10); err != nil {
		t.Fatal(err)
	}
	var count int64
	f.h.db.Model(&model.Subscription{}).Where("id = ?", fixed.SubscriptionID).Count(&count)
	if count != 0 {
		t.Fatal("instance not cleaned")
	}
	var order model.Order
	if err := f.h.db.First(&order, fixed.ID).Error; err != nil || order.SubscriptionEndReason != "exhausted" || order.SubscriptionEndedAt == nil || order.SubscriptionFinalFlowUsed != 1024 || order.Status != "paid" {
		t.Fatal("history lost", order, err)
	}
	if err := f.h.db.First(&model.Subscription{}, healthy.SubscriptionID).Error; err != nil {
		t.Fatal("healthy instance lost", err)
	}
}
