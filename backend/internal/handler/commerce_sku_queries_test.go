package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestCommerceSKUQueriesVisibilityAndAuthority(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql"} {
		t.Run(engine, func(t *testing.T) {
			var h *handlers
			if engine == "mysql" {
				h, _ = newMySQLPublishHandlers(t)
			} else {
				h, _ = newAnnouncementTestHandlers(t)
			}
			actor := model.User{Email: "sku-reader@example.test", Password: "unused", Status: "active", IsAdmin: true}
			if err := h.db.Create(&actor).Error; err != nil {
				t.Fatal(err)
			}
			group := model.NodeGroup{Name: "SKU queries", Code: "sku-queries", IsEnabled: true}
			if err := h.db.Create(&group).Error; err != nil {
				t.Fatal(err)
			}
			plan := model.Plan{Name: "SKU queries", Slug: "sku-queries", NodeGroupID: group.ID, IsActive: true}
			if err := h.db.Create(&plan).Error; err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			request := validCommerceSKURequest()
			request.Code = "query-purchase"
			purchase, err := h.services.SKUCreation.Create(ctx, actor.ID, plan.ID, request)
			if err != nil {
				t.Fatal(err)
			}
			request.Code = "query-renew"
			request.AllowedOperations = []string{"renew"}
			renewal, err := h.services.SKUCreation.Create(ctx, actor.ID, plan.ID, request)
			if err != nil {
				t.Fatal(err)
			}
			no := false
			request.Code = "query-hidden"
			request.IsActive = &no
			request.AllowedOperations = []string{"purchase"}
			hidden, err := h.services.SKUCreation.Create(ctx, actor.ID, plan.ID, request)
			if err != nil {
				t.Fatal(err)
			}
			q := commerce.SKUQuery{PlanID: plan.ID, Active: &no, Limit: 1}
			public, err := h.services.SKUQueries.Public(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			if public.Total != 2 || len(public.Items) != 1 || !public.Items[0].IsActive {
				t.Fatalf("public visibility: %+v", public)
			}
			q.Operation = "purchase"
			q.AnchorID = hidden.ID
			if _, err := h.services.SKUQueries.Public(ctx, q); !errors.Is(err, commerce.ErrNotFound) {
				t.Fatalf("hidden anchor: %v", err)
			}
			q.AnchorID = renewal.ID
			if _, err := h.services.SKUQueries.Public(ctx, q); !errors.Is(err, commerce.ErrNotFound) {
				t.Fatalf("operation anchor: %v", err)
			}
			q.AnchorID = purchase.ID
			public, err = h.services.SKUQueries.Public(ctx, q)
			if err != nil || public.Total != 1 || public.Items[0].ID != purchase.ID {
				t.Fatalf("scoped purchase: %+v %v", public, err)
			}
			q = commerce.SKUQuery{PlanID: plan.ID, LegacyType: "renewal"}
			public, err = h.services.SKUQueries.Public(ctx, q)
			if err != nil || public.Total != 1 || public.Items[0].ID != renewal.ID {
				t.Fatalf("legacy filter: %+v %v", public, err)
			}
			admin, err := h.services.SKUQueries.Administrative(ctx, actor.ID, commerce.SKUQuery{PlanID: plan.ID})
			if err != nil || admin.Total != 3 {
				t.Fatalf("admin: %+v %v", admin, err)
			}
			detail, err := h.services.SKUQueries.Get(ctx, actor.ID, hidden.ID)
			if err != nil || detail.IsActive {
				t.Fatalf("admin detail: %+v %v", detail, err)
			}
			_, err = h.services.SKUQueries.Public(ctx, commerce.SKUQuery{PlanID: plan.ID, Limit: 201})
			var invalid *commerce.ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("unbounded internal query: %v", err)
			}
			if _, err := h.services.SKUQueries.Administrative(ctx, 0, q); !errors.Is(err, commerce.ErrPermission) {
				t.Fatalf("anonymous admin: %v", err)
			}
			if err := h.db.Model(&plan).Update("is_active", false).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := h.services.SKUQueries.Public(ctx, commerce.SKUQuery{PlanID: plan.ID}); !errors.Is(err, commerce.ErrNotFound) {
				t.Fatalf("draft exposed: %v", err)
			}
			if err := h.db.Model(&actor).Update("is_admin", false).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := h.services.SKUQueries.Administrative(ctx, actor.ID, q); !errors.Is(err, commerce.ErrPermission) {
				t.Fatalf("revoked list: %v", err)
			}
			if _, err := h.services.SKUQueries.Get(ctx, actor.ID, purchase.ID); !errors.Is(err, commerce.ErrPermission) {
				t.Fatalf("revoked detail: %v", err)
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := h.services.SKUQueries.Public(canceled, q); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled: %v", err)
			}
		})
	}
}
