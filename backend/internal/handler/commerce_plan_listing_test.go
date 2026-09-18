package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestCommercePlanListingScopesAndLegacy(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql"} {
		t.Run(engine, func(t *testing.T) {
			var h *handlers
			if engine == "mysql" {
				h, _ = newMySQLPublishHandlers(t)
			} else {
				h, _ = newAnnouncementTestHandlers(t)
			}
			actor := model.User{Email: "catalog-admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
			if err := h.db.Create(&actor).Error; err != nil {
				t.Fatal(err)
			}
			group := model.NodeGroup{Name: "Catalog scope", Code: "catalog-scope", Description: "Saved group", IsEnabled: true}
			if err := h.db.Create(&group).Error; err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			var plans []model.Plan
			for i := 0; i < 3; i++ {
				plan := model.Plan{Name: fmt.Sprintf("List %d", i), Slug: fmt.Sprintf("list-%d", i), NodeGroupID: group.ID, IsActive: true, TrafficBytes: 1000}
				if err := h.db.Create(&plan).Error; err != nil {
					t.Fatal(err)
				}
				if i == 1 {
					if err := h.db.Model(&plan).Update("is_active", false).Error; err != nil {
						t.Fatal(err)
					}
				}
				plans = append(plans, plan)
				request := validCommerceSKURequest()
				request.Code = fmt.Sprintf("list-sku-%d", i)
				if i == 2 {
					request.AllowedOperations = []string{"renew"}
				}
				if _, err := h.services.SKUCreation.Create(ctx, actor.ID, plan.ID, request); err != nil {
					t.Fatal(err)
				}
			}
			request := validCommerceSKURequest()
			request.Code = "list-cheap-renew"
			request.PriceCents = 1
			request.AllowedOperations = []string{"renew"}
			if _, err := h.services.SKUCreation.Create(ctx, actor.ID, plans[0].ID, request); err != nil {
				t.Fatal(err)
			}
			no := false
			request.Code = "list-inactive"
			request.IsActive = &no
			if _, err := h.services.SKUCreation.Create(ctx, actor.ID, plans[0].ID, request); err != nil {
				t.Fatal(err)
			}
			q := commerce.PlanListQuery{IncludeInactive: true, Limit: 1, Operation: "purchase"}
			page, err := h.services.PlanListing.Public(ctx, q)
			if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != plans[0].ID || page.Items[0].SKUCount != 1 || page.Items[0].PrimarySKU.PriceCents != 1000 {
				t.Fatalf("public: %+v %v", page, err)
			}
			admin, err := h.services.PlanListing.Administrative(ctx, actor.ID, q)
			if err != nil || admin.Total != 1 || admin.Items[0].SKUCount != 1 {
				t.Fatalf("admin explicit scope: %+v %v", admin, err)
			}
			q.Operation = ""
			q.PlanID = plans[0].ID
			admin, err = h.services.PlanListing.Administrative(ctx, actor.ID, q)
			if err != nil || admin.Items[0].SKUCount != 3 || admin.Items[0].ActiveSKUCount != 2 || admin.Items[0].PrimarySKU.PriceCents != 1000 {
				t.Fatalf("management counts: %+v %v", admin, err)
			}
			q.ExcludePlanID = plans[0].ID
			page, err = h.services.PlanListing.Public(ctx, q)
			if err != nil || page.Total != 0 || page.Items == nil {
				t.Fatalf("exclusion: %+v %v", page, err)
			}
			q = commerce.PlanListQuery{IncludeInactive: true, LegacyArray: true}
			legacy, err := h.services.PlanListing.Public(ctx, q)
			if err != nil || len(legacy.Legacy) != 2 {
				t.Fatalf("public legacy: %+v %v", legacy, err)
			}
			for _, plan := range legacy.Legacy {
				if !plan.IsActive || plan.NodeGroup == nil || plan.NodeGroup.Description != group.Description {
					t.Fatalf("legacy plan: %+v", plan)
				}
				for _, sku := range plan.SKUs {
					if !sku.IsActive || sku.Code == "list-cheap-renew" {
						t.Fatalf("legacy SKU scope: %+v", sku)
					}
				}
			}
			legacy, err = h.services.PlanListing.Administrative(ctx, actor.ID, q)
			if err != nil || len(legacy.Legacy) != 3 {
				t.Fatalf("admin legacy: %+v %v", legacy, err)
			}
			q = commerce.PlanListQuery{Limit: 201}
			_, err = h.services.PlanListing.Public(ctx, q)
			var invalid *commerce.ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("unbounded page: %v", err)
			}
			if err := h.db.Model(&actor).Update("is_admin", false).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := h.services.PlanListing.Administrative(ctx, actor.ID, commerce.PlanListQuery{}); !errors.Is(err, commerce.ErrPermission) {
				t.Fatalf("revoked: %v", err)
			}
			token, _, err := h.issueToken(authClaims{UserID: actor.ID, Email: actor.Email, IsAdmin: true})
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			h.PlanListCommerceHandler(w, announcementRequest(http.MethodGet, "/api/v1/plans?paged=true&include_inactive=true", token, ""))
			var downgraded struct {
				Data struct {
					Items []commerce.PlanCatalog
					Total int64
				}
			}
			if err := json.Unmarshal(w.Body.Bytes(), &downgraded); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || downgraded.Data.Total != 2 {
				t.Fatalf("downgraded public HTTP: %d %s", w.Code, w.Body.String())
			}
			for _, item := range downgraded.Data.Items {
				if !item.IsActive {
					t.Fatal("revoked role exposed inactive product")
				}
			}

			w = httptest.NewRecorder()
			h.PlanListCommerceHandler(w, announcementRequest(http.MethodGet, "/api/v1/plans", "", ""))
			var response struct{ Data []commerce.LegacyPlan }
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || len(response.Data) != 2 {
				t.Fatalf("legacy HTTP: %d %s", w.Code, w.Body.String())
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := h.services.PlanListing.Public(canceled, commerce.PlanListQuery{}); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel: %v", err)
			}
		})
	}
}
