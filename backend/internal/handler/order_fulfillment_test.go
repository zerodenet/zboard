package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

type orderFixture struct {
	catalogFixture
	planRecord model.Plan
	skuRecord  model.PlanSKU
}

func newOrderFixture(t testing.TB) orderFixture {
	t.Helper()
	f := newCatalogFixture(t)
	return newOrderFixtureFromCatalog(t, f)
}

func newOrderFixtureFromCatalog(t testing.TB, f catalogFixture) orderFixture {
	t.Helper()
	plan := f.plan(t, 1)
	if err := f.h.db.Model(&plan).Update("traffic_bytes", 1024).Error; err != nil {
		t.Fatal(err)
	}
	sku := f.sku(t, plan.ID, 100, skuOperationPurchase)
	if err := f.h.db.Create(&model.PlanSKUOperation{PlanSKUID: sku.ID, Operation: skuOperationRenew}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := f.h.issueToken(authClaims{UserID: 1, Email: "reader@example.test", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	f.token = token
	return orderFixture{f, plan, sku}
}

func (f orderFixture) create(t testing.TB, target uint) model.Order {
	t.Helper()
	w := httptest.NewRecorder()
	f.h.OrderCreateCommerceValidatedHandler(w, announcementRequest(http.MethodPost, "/api/v1/orders", f.token,
		fmt.Sprintf(`{"plan_sku_id":%d,"target_subscription_id":%d}`, f.skuRecord.ID, target)))
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var response struct{ Data model.Order }
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}

func (f orderFixture) pay(t testing.TB, id uint, callback bool) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	path := fmt.Sprintf("/api/v1/admin/orders/%d/pay", id)
	if callback {
		f.h.OrderPayCallbackCommerceHandler(w, announcementRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%d/pay-callback", id), f.token, `{"status":"paid","raw_callback":"fixture"}`))
	} else {
		f.h.OrderPayCommerceHandler(w, announcementRequest(http.MethodPost, path, f.token, ""))
	}
	return w
}

func (f orderFixture) paid(t testing.TB, id uint) model.Order {
	t.Helper()
	w := f.pay(t, id, false)
	if w.Code != http.StatusOK {
		t.Fatalf("pay: %d %s", w.Code, w.Body.String())
	}
	var order model.Order
	if err := f.h.db.First(&order, id).Error; err != nil {
		t.Fatal(err)
	}
	return order
}

func TestNewOrderCreatesSeparateSubscriptionForSameSKU(t *testing.T) {
	f := newOrderFixture(t)
	first := f.paid(t, f.create(t, 0).ID)
	var before model.Subscription
	if err := f.h.db.First(&before, first.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	second := f.paid(t, f.create(t, 0).ID)
	if first.SubscriptionID == second.SubscriptionID {
		t.Fatal("new purchase silently renewed the existing subscription")
	}
	var after model.Subscription
	if err := f.h.db.First(&after, first.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if after.FlowTotal != before.FlowTotal || !after.EndAt.Equal(before.EndAt) {
		t.Fatal("new purchase changed existing entitlement")
	}
}

func TestPendingNewOrderRechecksCapacityAtSettlement(t *testing.T) {
	for _, callback := range []bool{false, true} {
		t.Run(fmt.Sprintf("callback=%t", callback), func(t *testing.T) {
			f := newOrderFixture(t)
			if err := f.h.db.Model(&f.planRecord).Update("max_active_subscriptions", 1).Error; err != nil {
				t.Fatal(err)
			}
			first, second := f.create(t, 0), f.create(t, 0)
			f.paid(t, first.ID)
			w := f.pay(t, second.ID, callback)
			if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), commerceErrorPlanSubscriptionLimitReached) {
				t.Fatalf("capacity exhausted: %d %s", w.Code, w.Body.String())
			}
			var actual model.Order
			if err := f.h.db.First(&actual, second.ID).Error; err != nil {
				t.Fatal(err)
			}
			if actual.Status != orderStatusPending || actual.SubscriptionID != 0 || actual.PaidAt != nil {
				t.Fatalf("failed settlement mutated order: %+v", actual)
			}
		})
	}
}

func TestExplicitRenewalPreservesTargetAndDoesNotAddQuotaForExtendOnly(t *testing.T) {
	f := newOrderFixture(t)
	first := f.paid(t, f.create(t, 0).ID)
	var before model.Subscription
	if err := f.h.db.First(&before, first.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	renewal := f.create(t, first.SubscriptionID)
	if renewal.OrderType != "renewal" {
		t.Fatalf("order type = %s", renewal.OrderType)
	}
	paid := f.paid(t, renewal.ID)
	var after model.Subscription
	if err := f.h.db.First(&after, first.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if paid.SubscriptionID != first.SubscriptionID || after.FlowTotal != before.FlowTotal || !after.EndAt.After(before.EndAt) {
		t.Fatalf("renewal changed target or quota: %+v", after)
	}
}

func TestSettlementRollsBackEntitlementWhenAuditFails(t *testing.T) {
	f := newOrderFixture(t)
	order := f.create(t, 0)
	if err := f.h.db.Exec(`CREATE TRIGGER reject_order_audit BEFORE INSERT ON audit_logs
BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if w := f.pay(t, order.ID, false); w.Code != http.StatusInternalServerError {
		t.Fatalf("audit failure: %d %s", w.Code, w.Body.String())
	}
	var actual model.Order
	if err := f.h.db.First(&actual, order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if actual.Status != orderStatusPending || actual.SubscriptionID != 0 || actual.PaidAt != nil {
		t.Fatal("settlement was partially committed")
	}
	for _, table := range []string{"subscriptions", "quota_events", "protocol_credentials"} {
		var count int64
		if err := f.h.db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
}

func TestCallbackCancellationPersistsCancellationTime(t *testing.T) {
	f := newOrderFixture(t)
	order := f.create(t, 0)
	w := httptest.NewRecorder()
	f.h.OrderPayCallbackCommerceHandler(w, announcementRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%d/pay-callback", order.ID), f.token, `{"status":"canceled"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("callback: %d %s", w.Code, w.Body.String())
	}
	var actual model.Order
	if err := f.h.db.First(&actual, order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if actual.Status != orderStatusCanceled || actual.CanceledAt == nil {
		t.Fatal("callback cancellation did not persist its timestamp")
	}
}
