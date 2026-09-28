package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestExpiredSubscriptionCanRenewWithinSevenDaysAndRecoversItsPeriodAndQuota(t *testing.T) {
	f := newOrderFixture(t)
	paid := f.paid(t, f.create(t, 0).ID)
	ended := time.Now().UTC().Add(-24 * time.Hour)
	if err := f.h.db.Model(&paid).Update("paid_at", ended.Add(-30*24*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Updates(map[string]any{"status": "expired", "end_at": ended, "ended_at": ended, "end_reason": "expired", "flow_used": 1024}).Error; err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []adminSubscriptionListItem
		Total int
	}
	if code := f.get(t, "/api/v1/subscriptions?paged=true&eligible_for=renew", f.h.SubscriptionsHandler, &page); code != 200 || page.Total != 1 || !page.Items[0].CanRenew || page.Items[0].CanReset || page.Items[0].RenewalUntil == nil {
		t.Fatal("renewal candidate", code, page)
	}
	if err := f.h.db.Model(&f.skuRecord).Update("price_cents", 200).Error; err != nil {
		t.Fatal(err)
	}
	renewed := f.paid(t, f.create(t, paid.SubscriptionID).ID)
	if renewed.SubscriptionID != paid.SubscriptionID {
		t.Fatal("renewal created another instance")
	}
	if renewed.SubscriptionEndedAt != nil || renewed.SubscriptionEndReason != "" {
		t.Fatal("paid recovery order still reports an ended subscription", renewed)
	}
	var history model.Order
	if err := f.h.db.First(&history, paid.ID).Error; err != nil || history.SubscriptionEndedAt == nil || history.SubscriptionEndReason != "expired" {
		t.Fatal("renewal erased prior terminal history", history, err)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	total, used := entitlements.CycleQuota(entitlements.Subscription(sub))
	if sub.Status != "active" || sub.EndedAt != nil || sub.EndReason != "" || !sub.EndAt.After(time.Now().UTC()) || total != 1024 || used != 0 || sub.FlowUsed != 1024 {
		t.Fatalf("recovery %+v total=%d used=%d", sub, total, used)
	}
	plan := f.plan(t, 2)
	if err := f.h.db.Model(&plan).Update("traffic_bytes", 2048).Error; err != nil {
		t.Fatal(err)
	}
	f.planRecord, f.skuRecord = plan, f.sku(t, plan.ID, 500, "change")
	quote := previewPricedOrder(t, f, sub.ID)
	if quote.TrafficCredit != renewed.PaidAmount || quote.CreditAmount < 199 || quote.CreditAmount > 200 || quote.PayableAmount != 500-quote.CreditAmount {
		t.Fatal("recovered service reused the ended period's paid value", quote, renewed)
	}
}

func TestRenewalWindowIsRecheckedAtCreationAndAtLateSettlement(t *testing.T) {
	f := newOrderFixture(t)
	paid := f.paid(t, f.create(t, 0).ID)
	ended := time.Now().UTC().Add(-24 * time.Hour)
	f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Updates(map[string]any{"status": "expired", "end_at": ended, "ended_at": ended, "end_reason": "expired"})
	pending := f.create(t, paid.SubscriptionID)
	tooOld := time.Now().UTC().Add(-8 * 24 * time.Hour)
	f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Updates(map[string]any{"end_at": tooOld, "ended_at": tooOld})
	if w := f.pay(t, pending.ID, false); w.Code == http.StatusOK {
		t.Fatal("late settlement restored service", w.Body.String())
	}
	w := httptest.NewRecorder()
	f.h.OrderCreateCommerceValidatedHandler(w, announcementRequest(http.MethodPost, "/api/v1/orders", f.token, fmt.Sprintf(`{"plan_sku_id":%d,"target_subscription_id":%d}`, f.skuRecord.ID, paid.SubscriptionID)))
	if w.Code == http.StatusOK {
		t.Fatal("late renewal creation accepted")
	}
	var page struct {
		Items []adminSubscriptionListItem
		Total int
	}
	if code := f.get(t, "/api/v1/subscriptions?paged=true&eligible_for=renew", f.h.SubscriptionsHandler, &page); code != 200 || page.Total != 0 {
		t.Fatal("late target listed", code, page)
	}
	var persisted model.Order
	f.h.db.First(&persisted, pending.ID)
	if persisted.Status != "pending" {
		t.Fatal("failed settlement mutated order", persisted)
	}
}
