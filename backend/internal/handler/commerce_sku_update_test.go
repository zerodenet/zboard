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
	"gorm.io/gorm"
)

func TestCommerceSKUUpdatePublicationAndAtomicity(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql"} {
		t.Run(engine, func(t *testing.T) {
			var h *handlers
			if engine == "mysql" {
				h, _ = newMySQLPublishHandlers(t)
			} else {
				h, _ = newAnnouncementTestHandlers(t)
			}
			actor := model.User{Email: "sku-editor@example.test", Password: "unused", Status: "active", IsAdmin: true}
			if err := h.db.Create(&actor).Error; err != nil {
				t.Fatal(err)
			}
			group := model.NodeGroup{Name: "Commerce", Code: "commerce-update", IsEnabled: true}
			if err := h.db.Create(&group).Error; err != nil {
				t.Fatal(err)
			}
			plan := model.Plan{Name: "Product", Slug: "sku-update", NodeGroupID: group.ID, IsActive: true}
			if err := h.db.Create(&plan).Error; err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			first := validCommerceSKURequest()
			first.Code = "update-first"
			second := first
			second.Code = "update-second"
			a, err := h.services.SKUCreation.Create(ctx, actor.ID, plan.ID, first)
			if err != nil {
				t.Fatal(err)
			}
			b, err := h.services.SKUCreation.Create(ctx, actor.ID, plan.ID, second)
			if err != nil {
				t.Fatal(err)
			}
			inactive := false
			first.IsActive = &inactive
			second.IsActive = &inactive
			start := make(chan struct{})
			results := make(chan error, 2)
			for _, input := range []struct {
				id      uint
				request commerce.SKURequest
			}{{a.ID, first}, {b.ID, second}} {
				go func(id uint, request commerce.SKURequest) {
					<-start
					_, err := h.services.SKUUpdate.Update(ctx, actor.ID, id, request)
					results <- err
				}(input.id, input.request)
			}
			close(start)
			success, denied := 0, 0
			for i := 0; i < 2; i++ {
				err := <-results
				var validation *commerce.ValidationError
				if err == nil {
					success++
				} else if errors.As(err, &validation) && validation.Fields["allowed_operations"] != "" {
					denied++
				} else {
					t.Errorf("update: %v", err)
				}
			}
			if success != 1 || denied != 1 {
				t.Fatalf("competing disables: success=%d denied=%d", success, denied)
			}
			var active, disabled model.PlanSKU
			if err := h.db.Where("plan_id = ? AND is_active = ?", plan.ID, true).First(&active).Error; err != nil {
				t.Fatal(err)
			}
			if err := h.db.Where("plan_id = ? AND is_active = ?", plan.ID, false).First(&disabled).Error; err != nil {
				t.Fatal(err)
			}
			input := validCommerceSKURequest()
			input.Code = active.Code
			input.AllowedOperations = []string{"renew"}
			if _, err := h.services.SKUUpdate.Update(ctx, actor.ID, active.ID, input); err == nil {
				t.Fatal("last purchase operation removed")
			}
			input.Code = active.Code
			input.AllowedOperations = []string{"purchase"}
			if _, err := h.services.SKUUpdate.Update(ctx, actor.ID, disabled.ID, input); !errors.Is(err, commerce.ErrSKUCodeConflict) {
				t.Fatalf("duplicate update: %v", err)
			}
			input.Code = disabled.Code
			input.Name = "must rollback"
			input.AllowedOperations = []string{"renew"}
			const callback = "commerce-update-audit-failure"
			if err := h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "audit_logs" {
					tx.AddError(errors.New("audit unavailable"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			view, err := h.services.SKUUpdate.Update(ctx, actor.ID, disabled.ID, input)
			h.db.Callback().Create().Remove(callback)
			if err == nil || view.ID != 0 {
				t.Fatalf("failed write result: %+v %v", view, err)
			}
			var after model.PlanSKU
			if err := h.db.First(&after, disabled.ID).Error; err != nil {
				t.Fatal(err)
			}
			if after.Name != disabled.Name || after.IsActive != disabled.IsActive {
				t.Fatal("SKU update survived audit rollback")
			}
			var operations []model.PlanSKUOperation
			if err := h.db.Where("plan_sku_id = ?", disabled.ID).Find(&operations).Error; err != nil {
				t.Fatal(err)
			}
			if len(operations) != 1 || operations[0].Operation != "purchase" {
				t.Fatalf("operations not rolled back: %+v", operations)
			}
			input = validCommerceSKURequest()
			input.Code = disabled.Code
			input.Name = "Saved"
			view, err = h.services.SKUUpdate.Update(ctx, actor.ID, disabled.ID, input)
			if err != nil {
				t.Fatal(err)
			}
			if view.ID != disabled.ID || view.PlanID != plan.ID || view.CreatedAt.IsZero() || view.UpdatedAt.IsZero() || !view.IsActive {
				t.Fatalf("update view: %+v", view)
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := h.services.SKUUpdate.Update(canceled, actor.ID, disabled.ID, input); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel: %v", err)
			}
			if err := h.db.Model(&actor).Update("is_admin", false).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := h.services.SKUUpdate.Update(ctx, actor.ID, disabled.ID, input); !errors.Is(err, commerce.ErrPermission) {
				t.Fatalf("revoked: %v", err)
			}
		})
	}
}

func TestCommercePublicationAndSKUDisableShareAuthority(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql"} {
		t.Run(engine, func(t *testing.T) {
			var h *handlers
			if engine == "mysql" {
				h, _ = newMySQLPublishHandlers(t)
			} else {
				h, _ = newAnnouncementTestHandlers(t)
			}
			actor := model.User{Email: "publish-editor@example.test", Password: "unused", Status: "active", IsAdmin: true}
			if err := h.db.Create(&actor).Error; err != nil {
				t.Fatal(err)
			}
			group := model.NodeGroup{Name: "Publish", Code: "publish-guard", IsEnabled: true}
			if err := h.db.Create(&group).Error; err != nil {
				t.Fatal(err)
			}
			plan := model.Plan{Name: "Publish", Slug: "publish-guard", NodeGroupID: group.ID}
			if err := h.db.Create(&plan).Error; err != nil {
				t.Fatal(err)
			}
			if err := h.db.Model(&plan).Update("is_active", false).Error; err != nil {
				t.Fatal(err)
			}
			attachCommerceEndpoint(t, h, group.ID)
			input := validCommerceSKURequest()
			input.Code = "publish-guard"
			sku, err := h.services.SKUCreation.Create(context.Background(), actor.ID, plan.ID, input)
			if err != nil {
				t.Fatal(err)
			}
			inactive := false
			input.IsActive = &inactive
			start := make(chan struct{})
			results := make(chan error, 2)
			go func() {
				<-start
				_, err := h.services.SKUUpdate.Update(context.Background(), actor.ID, sku.ID, input)
				results <- err
			}()
			go func() {
				<-start
				yes := true
				_, err := h.services.PlanUpdate.Update(context.Background(), actor.ID, plan.ID, commerce.PlanUpdateRequest{IsActive: &yes, ExpectedRevision: &plan.Revision})
				results <- err
			}()
			close(start)
			successes, denied := 0, 0
			for i := 0; i < 2; i++ {
				err := <-results
				var validation *commerce.ValidationError
				if err == nil {
					successes++
				} else if errors.As(err, &validation) {
					denied++
				} else {
					t.Errorf("publish race: %v", err)
				}
			}
			if successes != 1 || denied != 1 {
				t.Fatalf("publish/disable: %d %d", successes, denied)
			}
			if err := h.db.First(&plan, plan.ID).Error; err != nil {
				t.Fatal(err)
			}
			var stored model.PlanSKU
			if err := h.db.First(&stored, sku.ID).Error; err != nil {
				t.Fatal(err)
			}
			if plan.IsActive && !stored.IsActive {
				t.Fatal("published product without purchasable SKU")
			}
		})
	}
}

func TestCommerceSKUUpdateHTTPAliasesUseCoreRules(t *testing.T) {
	f := newCatalogFixture(t)
	plan := f.plan(t, 800)
	actor := model.User{Email: "sku-update-http@example.test", Password: "unused", Status: "active", IsAdmin: true}
	if err := f.h.db.Create(&actor).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := f.h.issueToken(authClaims{UserID: actor.ID, Email: actor.Email, IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	input := validCommerceSKURequest()
	input.Code = "update-http"
	sku, err := f.h.services.SKUCreation.Create(context.Background(), actor.ID, plan.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	input.AllowedOperations = []string{"renew"}
	body, _ := json.Marshal(input)
	path := fmt.Sprintf("/api/v1/admin/plan-skus/%d", sku.ID)
	for _, endpoint := range []http.HandlerFunc{f.h.PlanSKUUpdateCommerceHandler} {
		w := httptest.NewRecorder()
		endpoint(w, announcementRequest(http.MethodPut, path, token, string(body)))
		var response struct {
			Error struct{ Fields map[string]string }
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusBadRequest || response.Error.Fields["allowed_operations"] == "" {
			t.Fatalf("publication rule: %d %s", w.Code, w.Body.String())
		}
	}
	input.AllowedOperations = []string{"purchase", "renew"}
	input.Name = "Updated"
	body, _ = json.Marshal(input)
	w := httptest.NewRecorder()
	f.h.PlanSKUUpdateCommerceHandler(w, announcementRequest(http.MethodPut, path, token, string(body)))
	var response struct{ Data commerce.SKUView }
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || response.Data.ID != sku.ID || response.Data.Name != "Updated" || len(response.Data.AllowedOperations) != 2 {
		t.Fatalf("update response: %d %s", w.Code, w.Body.String())
	}
}
