package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

type catalogFixture struct {
	h     *handlers
	token string
	group model.NodeGroup
}

func newCatalogFixture(t *testing.T) catalogFixture {
	t.Helper()
	h, token := newAnnouncementTestHandlers(t) // Full SQLite schema and an authenticated account.
	group := model.NodeGroup{Name: "Catalog", Code: "catalog", IsEnabled: true}
	if err := h.db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	return catalogFixture{h, token, group}
}

func (f catalogFixture) plan(t *testing.T, id uint) model.Plan {
	t.Helper()
	plan := model.Plan{ID: id, Name: fmt.Sprintf("Plan %d", id), Slug: fmt.Sprintf("plan-%d", id), NodeGroupID: f.group.ID, IsActive: true}
	if err := f.h.db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	return plan
}

func (f catalogFixture) sku(t *testing.T, planID uint, price int64, operation string) model.PlanSKU {
	t.Helper()
	var count int64
	f.h.db.Model(&model.PlanSKU{}).Count(&count)
	sku := model.PlanSKU{PlanID: planID, Code: fmt.Sprintf("sku-%d", count+1), Name: "SKU", BillingUnit: "month", BillingValue: 1, PriceCents: price, Currency: "CNY", IsActive: true}
	if err := f.h.db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.PlanSKUOperation{PlanSKUID: sku.ID, Operation: operation}).Error; err != nil {
		t.Fatal(err)
	}
	return sku
}

func (f catalogFixture) get(t *testing.T, path string, handler http.HandlerFunc, data any) int {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler(recorder, announcementRequest(http.MethodGet, path, f.token, ""))
	if data != nil && recorder.Code == http.StatusOK {
		var response struct{ Data json.RawMessage }
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(response.Data, data); err != nil {
			t.Fatal(err)
		}
	}
	return recorder.Code
}

func TestCatalogProjectionFiltersBeforePagingAndSelectsOperationPrice(t *testing.T) {
	f := newCatalogFixture(t)
	for id := uint(1); id <= 4; id++ {
		f.plan(t, id)
		f.sku(t, id, 1, skuOperationPurchase)
		if id < 4 {
			f.sku(t, id, 30, skuOperationChange)
			f.sku(t, id, 20, skuOperationChange)
		}
	}
	for offset, wantID := range []uint{3, 2} {
		var page struct {
			Items []planCatalogItem
			Total int64
		}
		path := fmt.Sprintf("/api/v1/plans?paged=true&operation=change&exclude_plan_id=1&limit=1&offset=%d", offset)
		if status := f.get(t, path, f.h.PlanListCommerceHandler, &page); status != http.StatusOK {
			t.Fatalf("status = %d", status)
		}
		if page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != wantID {
			t.Fatalf("unexpected filtered page: %+v", page)
		}
		item := page.Items[0]
		if item.ActiveSKUCount != 2 || item.PrimarySKU == nil || item.PrimarySKU.PriceCents != 20 {
			t.Fatalf("operation price/count = %+v", item)
		}
	}
	if status := f.get(t, "/api/v1/plans?paged=true&operation=invalid", f.h.PlanListCommerceHandler, nil); status != http.StatusBadRequest {
		t.Fatalf("invalid operation status = %d", status)
	}
}

func TestCatalogSKUAnchorFindsSelectedSKUAfterFirstHundredWithinScope(t *testing.T) {
	f := newCatalogFixture(t)
	f.plan(t, 1)
	var selected model.PlanSKU
	for index := 0; index < 105; index++ {
		selected = f.sku(t, 1, int64(index), skuOperationPurchase)
	}
	var page struct {
		Items  []commercePlanSKUItem
		Offset int
		Total  int
	}
	path := fmt.Sprintf("/api/v1/plans/1/skus?operation=purchase&anchor_id=%d&limit=25", selected.ID)
	if status := f.get(t, path, f.h.PublicPlanSKUListCommerceHandler, &page); status != http.StatusOK {
		t.Fatalf("anchor status = %d", status)
	}
	if page.Offset != 100 || page.Total != 105 || len(page.Items) != 5 || page.Items[4].ID != selected.ID {
		t.Fatalf("anchor page = offset %d total %d items %+v", page.Offset, page.Total, page.Items)
	}
	f.plan(t, 2)
	other := f.sku(t, 2, 1, skuOperationPurchase)
	for _, suffix := range []string{fmt.Sprintf("anchor_id=%d", other.ID), fmt.Sprintf("operation=renew&anchor_id=%d", selected.ID)} {
		if status := f.get(t, "/api/v1/plans/1/skus?"+suffix, f.h.PublicPlanSKUListCommerceHandler, nil); status != http.StatusNotFound {
			t.Fatalf("out-of-scope anchor status = %d", status)
		}
	}
}

func TestSubscriptionManagementPagingAndExactIDRemainAccountScoped(t *testing.T) {
	f := newCatalogFixture(t)
	f.plan(t, 1)
	sku := f.sku(t, 1, 1, skuOperationPurchase)
	now := time.Now().UTC()
	for id := uint(1); id <= 105; id++ {
		sub := model.Subscription{ID: id, UserID: 1, PlanID: 1, PlanSKUID: sku.ID, NodeGroupID: f.group.ID, Status: subStatusActive, StartAt: now, EndAt: now.Add(time.Hour), FlowTotal: 100}
		if err := f.h.db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
	}
	other := model.User{Email: "other@example.test", Password: "unused", Status: userStatusActive}
	if err := f.h.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	permanent := model.Subscription{UserID: other.ID, PlanID: 1, PlanSKUID: sku.ID, NodeGroupID: f.group.ID, Status: subStatusExpired, StartAt: now, EndAt: perpetualSubscriptionEnd, FlowTotal: 100, FlowUsed: 100}
	if err := f.h.db.Create(&permanent).Error; err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []adminSubscriptionListItem
		Total int
	}
	for _, purpose := range []string{"manage", "renew", "change", "addon"} {
		path := "/api/v1/subscriptions?paged=true&eligible_for=" + purpose + "&limit=25&offset=100"
		if status := f.get(t, path, f.h.SubscriptionsHandler, &page); status != http.StatusOK || page.Total != 105 || len(page.Items) != 5 {
			t.Fatalf("management page status=%d total=%d rows=%d", status, page.Total, len(page.Items))
		}
	}
	if status := f.get(t, "/api/v1/subscriptions?paged=true&subscription_id=1&limit=1", f.h.SubscriptionsHandler, &page); status != http.StatusOK || len(page.Items) != 1 || page.Items[0].ID != 1 {
		t.Fatalf("exact lookup status=%d page=%+v", status, page)
	}
	path := fmt.Sprintf("/api/v1/subscriptions?paged=true&eligible_for=manage&subscription_id=%d", permanent.ID)
	if status := f.get(t, path, f.h.SubscriptionsHandler, &page); status != http.StatusOK || page.Total != 0 {
		t.Fatalf("other account lookup status=%d total=%d", status, page.Total)
	}
	if status := f.get(t, "/api/v1/subscriptions?paged=true&eligible_for=invalid", f.h.SubscriptionsHandler, nil); status != http.StatusBadRequest {
		t.Fatalf("invalid purpose status=%d", status)
	}
}

func TestSubscriptionManagementFilterPreservesPermanentRecoveryBoundary(t *testing.T) {
	f := newCatalogFixture(t)
	f.plan(t, 1)
	sku := f.sku(t, 1, 1, skuOperationPurchase)
	now := time.Now().UTC()
	for index, item := range []struct {
		status string
		end    time.Time
		used   int64
	}{
		{subStatusActive, now.Add(time.Hour), 0},
		{subStatusExpired, perpetualSubscriptionEnd, 100},
		{subStatusActive, perpetualSubscriptionEnd, 100},
		{subStatusCanceled, perpetualSubscriptionEnd, 100},
		{subStatusExpired, now.Add(-time.Hour), 0},
		{subStatusActive, now.Add(time.Hour), 100},
	} {
		sub := model.Subscription{ID: uint(index + 1), UserID: 1, PlanID: 1, PlanSKUID: sku.ID,
			NodeGroupID: f.group.ID, Status: item.status, StartAt: now, EndAt: item.end, FlowTotal: 100, FlowUsed: item.used}
		if err := f.h.db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, purpose := range []string{"manage", "renew", "change", "addon"} {
		var page struct {
			Items []adminSubscriptionListItem
			Total int
		}
		want := 1
		if purpose == "manage" || purpose == "renew" {
			want = 3
		}
		status := f.get(t, "/api/v1/subscriptions?paged=true&eligible_for="+purpose, f.h.SubscriptionsHandler, &page)
		if status != http.StatusOK || page.Total != want {
			t.Fatalf("%s status=%d total=%d want=%d", purpose, status, page.Total, want)
		}
		for _, item := range page.Items {
			if item.ID > 3 {
				t.Fatalf("ineligible subscription included: %d", item.ID)
			}
		}
	}
}

func TestExplicitCatalogOperationKeepsAdminStorefrontScoped(t *testing.T) {
	f := newCatalogFixture(t)
	f.plan(t, 1)
	f.plan(t, 2)
	f.plan(t, 3)
	f.sku(t, 1, 10, skuOperationPurchase)
	f.sku(t, 2, 10, skuOperationRenew)
	f.sku(t, 3, 10, skuOperationPurchase)
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := f.h.issueToken(authClaims{UserID: 1, Email: "reader@example.test", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	f.token = token
	if err := f.h.db.Model(&model.Plan{}).Where("id = ?", 3).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []planCatalogItem
		Total int
	}
	status := f.get(t, "/api/v1/plans?paged=true&operation=purchase&include_inactive=true", f.h.PlanListCommerceHandler, &page)
	if status != http.StatusOK || page.Total != 1 || page.Items[0].ID != 1 {
		t.Fatalf("explicit storefront status=%d page=%+v", status, page)
	}
	status = f.get(t, "/api/v1/plans?paged=true&operation=renew&plan_id=2", f.h.PlanListCommerceHandler, &page)
	if status != http.StatusOK || page.Total != 1 || page.Items[0].ID != 2 {
		t.Fatalf("renew plan scope status=%d page=%+v", status, page)
	}
	status = f.get(t, "/api/v1/plans?paged=true&include_inactive=true", f.h.PlanListCommerceHandler, &page)
	if status != http.StatusOK || page.Total != 3 {
		t.Fatalf("admin management status=%d total=%d", status, page.Total)
	}
}
