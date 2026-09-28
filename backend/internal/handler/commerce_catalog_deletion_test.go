package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func deleteCatalog(t *testing.T, f orderFixture, id uint, plan bool) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	path := fmt.Sprintf("/api/v1/admin/plan-skus/%d", id)
	handler := f.h.PlanSKUDeleteCommerceHandler
	if plan {
		path = fmt.Sprintf("/api/v1/admin/plans/%d", id)
		handler = f.h.PlanDeleteCommerceHandler
	}
	handler(w, announcementRequest(http.MethodDelete, path, f.token, ""))
	return w
}

func TestCatalogDeletionPreservesSubscriptionsAndPendingOrderSnapshots(t *testing.T) {
	for _, plan := range []bool{false, true} {
		t.Run(fmt.Sprintf("plan=%t", plan), func(t *testing.T) {
			f := newOrderFixture(t)
			existing := f.paid(t, f.create(t, 0).ID)
			pending := f.create(t, 0)
			var before model.Subscription
			if err := f.h.db.First(&before, existing.SubscriptionID).Error; err != nil {
				t.Fatal(err)
			}
			id := f.skuRecord.ID
			if plan {
				id = f.planRecord.ID
			}
			for i := 0; i < 2; i++ {
				if w := deleteCatalog(t, f, id, plan); w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			var sku model.PlanSKU
			if err := f.h.db.First(&sku, f.skuRecord.ID).Error; err != nil || sku.ArchivedAt == nil || sku.IsActive {
				t.Fatal("SKU archive", sku, err)
			}
			var product model.Plan
			f.h.db.First(&product, f.planRecord.ID)
			if product.IsActive || (plan && product.ArchivedAt == nil) {
				t.Fatal("product still published", product)
			}
			var after model.Subscription
			f.h.db.First(&after, before.ID)
			if before != after {
				t.Fatal("existing service changed", before, after)
			}
			if code := f.get(t, fmt.Sprintf("/api/v1/admin/plan-skus/%d", sku.ID), f.h.PlanSKUGetCommerceHandler, nil); code != 404 {
				t.Fatal("deleted SKU visible", code)
			}
			if plan {
				if code := f.get(t, fmt.Sprintf("/api/v1/admin/plans/%d", product.ID), f.h.PlanDetailHandler, nil); code != 404 {
					t.Fatal("deleted product visible", code)
				}
			} else {
				var page struct {
					Items []commercePlanSKUItem
					Total int64
				}
				if code := f.get(t, fmt.Sprintf("/api/v1/admin/plans/%d/skus", product.ID), f.h.PlanSKUListCommerceHandler, &page); code != 200 || page.Total != 0 {
					t.Fatal("deleted SKU listed", code, page)
				}
			}
			// An order already placed keeps the commercial terms captured at creation.
			settled := f.paid(t, pending.ID)
			if settled.SubscriptionID == 0 {
				t.Fatal("pending order lost fulfillment")
			}
			var created model.Subscription
			f.h.db.First(&created, settled.SubscriptionID)
			if created.FlowTotal != pending.TrafficBytes || created.PlanID != pending.PlanID || created.PlanSKUID != pending.PlanSKUID {
				t.Fatal("order snapshot lost", created, pending)
			}
			w := httptest.NewRecorder()
			f.h.OrderCreateCommerceValidatedHandler(w, announcementRequest(http.MethodPost, "/api/v1/orders", f.token, fmt.Sprintf(`{"plan_sku_id":%d}`, sku.ID)))
			if w.Code == 200 {
				t.Fatal("deleted SKU still sold")
			}
			var count int64
			action := "plan.sku.delete"
			if plan {
				action = "plan.delete"
			}
			f.h.db.Model(&model.AuditLog{}).Where("action = ?", action).Count(&count)
			if count != 1 {
				t.Fatal("deletion audit replayed", count)
			}
		})
	}
}

func TestCatalogDeletionRechecksAdministratorAndAllowsInactiveSKU(t *testing.T) {
	f := newOrderFixture(t)
	f.h.db.Model(&f.skuRecord).Update("is_active", false)
	f.h.db.Model(&model.User{}).Where("id = 1").Update("is_admin", false)
	if w := deleteCatalog(t, f, f.skuRecord.ID, false); w.Code != 403 {
		t.Fatal(w.Code)
	}
	var sku model.PlanSKU
	f.h.db.First(&sku, f.skuRecord.ID)
	if sku.ArchivedAt != nil {
		t.Fatal("unauthorized archive")
	}
	f.h.db.Model(&model.User{}).Where("id = 1").Update("is_admin", true)
	if w := deleteCatalog(t, f, sku.ID, false); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
