package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func planCreationRequest(group uint) commerce.PlanCreateRequest {
	no := false
	first := validCommerceSKURequest()
	first.Code = "create-first"
	first.IsActive = &no
	first.SortOrder = 2
	second := validCommerceSKURequest()
	second.Code = "create-second"
	second.SortOrder = 1
	return commerce.PlanCreateRequest{Name: " Draft ", Slug: " CREATE-PLAN ", NodeGroupID: group, TrafficBytes: 1024, DeviceLimit: 2, IsActive: false, IsRenewable: &no, SKUs: []commerce.SKURequest{first, second}}
}
func TestCommercePlanCreationAtomicityAndDefaults(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql"} {
		t.Run(engine, func(t *testing.T) {
			var h *handlers
			if engine == "mysql" {
				h, _ = newMySQLPublishHandlers(t)
			} else {
				h, _ = newAnnouncementTestHandlers(t)
			}
			actor := model.User{Email: "plan-creator@example.test", Password: "unused", Status: "active", IsAdmin: true}
			if err := h.db.Create(&actor).Error; err != nil {
				t.Fatal(err)
			}
			group := model.NodeGroup{Name: "Create", Code: "create-plan", IsEnabled: true}
			if err := h.db.Create(&group).Error; err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			request := planCreationRequest(group.ID)
			view, err := h.services.PlanCreation.Create(ctx, actor.ID, request)
			if err != nil {
				t.Fatal(err)
			}
			if view.ID == 0 || view.IsActive || view.IsRenewable || view.Name != "Draft" || view.Slug != "create-plan" || len(view.SKUs) != 2 || view.SKUs[0].Code != "create-second" || view.SKUs[1].IsActive {
				t.Fatalf("draft defaults/order: %+v", view)
			}
			var stored model.Plan
			if err := h.db.First(&stored, view.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.IsActive || stored.IsRenewable {
				t.Fatal("defaults activated draft")
			}
			_, err = h.services.PlanCreation.Create(ctx, actor.ID, request)
			var conflict *commerce.IdentifierConflict
			if !errors.As(err, &conflict) || len(conflict.Fields) != 3 {
				t.Fatalf("aggregate conflict: %v", err)
			}
			if engine == "mysql" {
				duplicateName := planCreationRequest(group.ID)
				duplicateName.Slug = "distinct-slug"
				duplicateName.SKUs[0].Code = "distinct-first"
				duplicateName.SKUs[1].Code = "distinct-second"
				_, err := h.services.PlanCreation.Create(ctx, actor.ID, duplicateName)
				if !errors.As(err, &conflict) || conflict.Fields["name"] == "" || conflict.Fields["slug"] != "" {
					t.Fatalf("legacy name conflict: %v", err)
				}
			}
			request.Name = "Rollback test"
			request.Slug = "rollback-plan"
			request.SKUs[0].Code = "rollback-first"
			request.SKUs[1].Code = "rollback-second"
			const callback = "plan-create-audit-failure"
			if err := h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "audit_logs" {
					tx.AddError(errors.New("audit unavailable"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			failed, err := h.services.PlanCreation.Create(ctx, actor.ID, request)
			h.db.Callback().Create().Remove(callback)
			if err == nil || failed.ID != 0 {
				t.Fatalf("failure result: %+v %v", failed, err)
			}
			for _, entry := range []struct {
				model interface{}
				want  int64
			}{{&model.Plan{}, 1}, {&model.PlanSKU{}, 2}, {&model.PlanSKUOperation{}, 2}} {
				var count int64
				if err := h.db.Model(entry.model).Count(&count).Error; err != nil || count != entry.want {
					t.Fatalf("rollback %T: %d %v", entry.model, count, err)
				}
			}
			request.IsActive = true
			_, err = h.services.PlanCreation.Create(ctx, actor.ID, request)
			var invalid *commerce.ValidationError
			if !errors.As(err, &invalid) || invalid.Fields["node_group_id"] == "" {
				t.Fatalf("publication without endpoints: %v", err)
			}
			attachCommerceEndpoint(t, h, group.ID)
			published, err := h.services.PlanCreation.Create(ctx, actor.ID, request)
			if err != nil || !published.IsActive {
				t.Fatalf("publication: %+v %v", published, err)
			}
			request.Name = "Competing test"
			request.Slug = "competing-create"
			request.SKUs[0].Code = "competing-first"
			request.SKUs[1].Code = "competing-second"
			start := make(chan struct{})
			results := make(chan error, 2)
			for i := 0; i < 2; i++ {
				go func() { <-start; _, err := h.services.PlanCreation.Create(ctx, actor.ID, request); results <- err }()
			}
			close(start)
			success, denied := 0, 0
			for i := 0; i < 2; i++ {
				err := <-results
				if err == nil {
					success++
				} else if errors.As(err, &conflict) {
					denied++
				} else {
					t.Errorf("concurrent create: %v", err)
				}
			}
			if success != 1 || denied != 1 {
				t.Fatalf("creation competition: %d %d", success, denied)
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := h.services.PlanCreation.Create(canceled, actor.ID, request); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel: %v", err)
			}
			if err := h.db.Model(&actor).Update("is_admin", false).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := h.services.PlanCreation.Create(ctx, actor.ID, request); !errors.Is(err, commerce.ErrPermission) {
				t.Fatalf("revoked: %v", err)
			}
		})
	}
}
func TestCommercePlanCreationHTTPFieldContract(t *testing.T) {
	f := newOrderFixture(t)
	request := planCreationRequest(f.group.ID)
	request.SKUs[0].PriceCents = -1
	body, _ := json.Marshal(request)
	w := httptest.NewRecorder()
	f.h.PlanCreateCommerceHandler(w, announcementRequest(http.MethodPost, "/api/v1/plans", f.token, string(body)))
	var failure struct {
		Error struct{ Fields map[string]string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusBadRequest || failure.Error.Fields["skus.0.price_cents"] == "" {
		t.Fatalf("nested field: %d %s", w.Code, w.Body.String())
	}
	request.SKUs[0].PriceCents = 0
	body, _ = json.Marshal(request)
	w = httptest.NewRecorder()
	f.h.PlanCreateCommerceHandler(w, announcementRequest(http.MethodPost, "/api/v1/plans", f.token, string(body)))
	var response struct{ Data commerce.Plan }
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || response.Data.ID == 0 || response.Data.IsActive || response.Data.IsRenewable {
		t.Fatalf("draft HTTP: %d %s", w.Code, w.Body.String())
	}
}
