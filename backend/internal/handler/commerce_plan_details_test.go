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

func TestCommercePlanDetailsScopeAndPrice(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql"} {
		t.Run(engine, func(t *testing.T) {
			var h *handlers
			if engine == "mysql" {
				h, _ = newMySQLPublishHandlers(t)
			} else {
				h, _ = newAnnouncementTestHandlers(t)
			}
			actor := model.User{Email: "plan-details@example.test", Password: "unused", Status: "active", IsAdmin: true}
			if err := h.db.Create(&actor).Error; err != nil {
				t.Fatal(err)
			}
			group := model.NodeGroup{Name: "Details", Code: "plan-details", IsEnabled: true}
			if err := h.db.Create(&group).Error; err != nil {
				t.Fatal(err)
			}
			plan := model.Plan{Name: "Details", Slug: "plan-details", Description: "Saved description", NodeGroupID: group.ID, IsActive: true, TrafficBytes: 12345, DeviceLimit: 4, SpeedLimitMbps: 20}
			if err := h.db.Create(&plan).Error; err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			var primaryID uint
			for i, input := range []struct {
				price     int64
				operation string
				active    bool
			}{{1, "renew", true}, {1, "purchase", false}, {200, "purchase", true}, {100, "purchase", true}, {100, "purchase", true}} {
				request := validCommerceSKURequest()
				request.Code = fmt.Sprintf("detail-%d", i)
				request.PriceCents = input.price
				request.AllowedOperations = []string{input.operation}
				request.IsActive = &input.active
				sku, err := h.services.SKUCreation.Create(ctx, actor.ID, plan.ID, request)
				if err != nil {
					t.Fatal(err)
				}
				if i == 3 {
					primaryID = sku.ID
				}
			}
			public, err := h.services.PlanDetails.Public(ctx, plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			if public.PrimarySKU == nil || public.PrimarySKU.ID != primaryID || public.SKUCount != 3 || public.ActiveSKUCount != 3 {
				t.Fatalf("public price/counts: %+v", public)
			}
			if public.TrafficBytes != plan.TrafficBytes || public.DeviceLimit != plan.DeviceLimit || public.SpeedLimitMbps != plan.SpeedLimitMbps || public.Description != plan.Description || public.NodeGroup == nil || public.NodeGroup.ID != group.ID {
				t.Fatalf("public facts: %+v", public)
			}
			admin, err := h.services.PlanDetails.Administrative(ctx, actor.ID, plan.ID)
			if err != nil || admin.SKUCount != 5 || admin.ActiveSKUCount != 4 {
				t.Fatalf("admin counts: %+v %v", admin, err)
			}
			token, _, err := h.issueToken(authClaims{UserID: actor.ID, Email: actor.Email, IsAdmin: true})
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			h.PublicPlanDetailCommerceHandler(w, announcementRequest(http.MethodGet, fmt.Sprintf("/api/v1/plans/%d", plan.ID), token, ""))
			var response struct{ Data commerce.PlanCatalog }
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || response.Data.SKUCount != 3 {
				t.Fatalf("admin token widened public detail: %d %s", w.Code, w.Body.String())
			}
			w = httptest.NewRecorder()
			h.PlanDetailHandler(w, announcementRequest(http.MethodGet, fmt.Sprintf("/api/v1/admin/plans/%d", plan.ID), token, ""))
			var detail struct{ Data commerce.PlanDetail }
			if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || detail.Data.SKUCount != 5 {
				t.Fatalf("admin HTTP: %d %s", w.Code, w.Body.String())
			}
			empty := model.Plan{Name: "Empty details", Slug: "empty-details", NodeGroupID: group.ID, IsActive: true}
			if err := h.db.Create(&empty).Error; err != nil {
				t.Fatal(err)
			}
			public, err = h.services.PlanDetails.Public(ctx, empty.ID)
			if err != nil || public.PrimarySKU != nil || public.SKUCount != 0 {
				t.Fatalf("historical empty: %+v %v", public, err)
			}
			if err := h.db.Model(&plan).Update("is_active", false).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := h.services.PlanDetails.Public(ctx, plan.ID); !errors.Is(err, commerce.ErrNotFound) {
				t.Fatalf("draft exposed: %v", err)
			}
			if _, err := h.services.PlanDetails.Administrative(ctx, 0, plan.ID); !errors.Is(err, commerce.ErrPermission) {
				t.Fatalf("anonymous: %v", err)
			}
			if err := h.db.Model(&actor).Update("is_admin", false).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := h.services.PlanDetails.Administrative(ctx, actor.ID, plan.ID); !errors.Is(err, commerce.ErrPermission) {
				t.Fatalf("revoked: %v", err)
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := h.services.PlanDetails.Public(canceled, empty.ID); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled: %v", err)
			}
		})
	}
}
