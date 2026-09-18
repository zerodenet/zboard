package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestCommerceSKUCreationAuthorityAndAtomicity(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql"} {
		t.Run(engine, func(t *testing.T) {
			var h *handlers
			if engine == "mysql" {
				h, _ = newMySQLPublishHandlers(t)
			} else {
				h, _ = newAnnouncementTestHandlers(t)
			}
			actor := model.User{Email: "commerce-owner@example.test", Password: "unused", Status: "active", IsAdmin: true}
			if err := h.db.Create(&actor).Error; err != nil {
				t.Fatal(err)
			}
			group := model.NodeGroup{Name: "commerce", Code: "commerce", IsEnabled: true}
			if err := h.db.Create(&group).Error; err != nil {
				t.Fatal(err)
			}
			plan := model.Plan{Name: "Product", Slug: "creation-test", NodeGroupID: group.ID}
			if err := h.db.Create(&plan).Error; err != nil {
				t.Fatal(err)
			}
			request := validCommerceSKURequest()
			inactive := false
			request.IsActive = &inactive
			request.AllowedOperations = []string{"renew", "purchase", "renew"}
			ctx := context.Background()
			view, err := h.services.SKUCreation.Create(ctx, actor.ID, plan.ID, request)
			if err != nil {
				t.Fatal(err)
			}
			var stored model.PlanSKU
			if err := h.db.First(&stored, view.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.IsActive || view.IsActive || view.ID == 0 || len(view.AllowedOperations) != 2 {
				t.Fatalf("creation mismatch: %+v", view)
			}
			var count int64
			if err := h.db.Model(&model.PlanSKUOperation{}).Where("plan_sku_id = ?", view.ID).Count(&count).Error; err != nil || count != 2 {
				t.Fatalf("operations: %d %v", count, err)
			}
			if err := h.db.Model(&model.AuditLog{}).Where("action = ? AND user_id = ?", "plan.sku.create", actor.ID).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("audit: %d %v", count, err)
			}
			// The database unique constraint is authoritative, including concurrent callers.
			duplicate, duplicateErr := h.services.SKUCreation.Create(ctx, actor.ID, plan.ID, request)
			if !errors.Is(duplicateErr, commerce.ErrSKUCodeConflict) || duplicate.ID != 0 {
				t.Fatalf("duplicate classification: %+v %v", duplicate, duplicateErr)
			}
			// Inject failure at the final write, after SKU and operations were inserted.
			const callback = "commerce-test-fail-audit"
			if err := h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "audit_logs" {
					tx.AddError(errors.New("audit unavailable"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			request.Code = "rollback-sku"
			failed, err := h.services.SKUCreation.Create(ctx, actor.ID, plan.ID, request)
			h.db.Callback().Create().Remove(callback)
			if err == nil || failed.ID != 0 {
				t.Fatalf("failure exposed committed result: %+v %v", failed, err)
			}
			if err := h.db.Model(&model.PlanSKU{}).Where("code = ?", request.Code).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("SKU not rolled back: %d %v", count, err)
			}
			if err := h.db.Model(&model.PlanSKUOperation{}).Count(&count).Error; err != nil || count != 2 {
				t.Fatalf("operation rollback: %d %v", count, err)
			}
			request.Code = "competing-sku"
			results := make(chan error, 2)
			start := make(chan struct{})
			for i := 0; i < 2; i++ {
				go func() {
					<-start
					_, err := h.services.SKUCreation.Create(ctx, actor.ID, plan.ID, request)
					results <- err
				}()
			}
			close(start)
			success, conflicts := 0, 0
			for i := 0; i < 2; i++ {
				err := <-results
				if err == nil {
					success++
				} else if errors.Is(err, commerce.ErrSKUCodeConflict) {
					conflicts++
				} else {
					t.Fatalf("concurrent create: %v", err)
				}
			}
			if success != 1 || conflicts != 1 {
				t.Fatalf("competing results: success=%d conflict=%d", success, conflicts)
			}
			if err := h.db.Model(&model.PlanSKU{}).Where("code = ?", request.Code).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("competing rows: %d %v", count, err)
			}
			if _, err := h.services.SKUCreation.Create(ctx, 0, plan.ID, request); !errors.Is(err, commerce.ErrPermission) {
				t.Fatalf("anonymous: %v", err)
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := h.services.SKUCreation.Create(canceled, actor.ID, plan.ID, request); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel: %v", err)
			}
			if err := h.db.Model(&actor).Update("is_admin", false).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := h.services.SKUCreation.Create(ctx, actor.ID, plan.ID, request); !errors.Is(err, commerce.ErrPermission) {
				t.Fatalf("revoked authority: %v", err)
			}
		})
	}
}

func TestCommerceSKUCreationHTTPUsesCoreContract(t *testing.T) {
	f := newCatalogFixture(t)
	plan := f.plan(t, 700)
	actor := model.User{Email: "commerce-http@example.test", Password: "unused", Status: "active", IsAdmin: true}
	if err := f.h.db.Create(&actor).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := f.h.issueToken(authClaims{UserID: actor.ID, Email: actor.Email, IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	request := validCommerceSKURequest()
	request.Code = "http-sku"
	inactive := false
	request.IsActive = &inactive
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	f.h.PlanSKUCreateCommerceHandler(w, announcementRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/plans/%d/skus", plan.ID), token, string(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP create: %d %s", w.Code, w.Body.String())
	}
	var response struct{ Data commerce.SKUView }
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.ID == 0 || response.Data.IsActive || response.Data.PlanID != plan.ID || len(response.Data.AllowedOperations) != 1 {
		t.Fatalf("HTTP contract: %+v", response.Data)
	}
	w = httptest.NewRecorder()
	f.h.PlanSKUCreateCommerceHandler(w, announcementRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/plans/%d/skus", plan.ID), token, string(body)))
	var conflict struct {
		Error struct {
			Code   string
			Fields map[string]string
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &conflict); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusBadRequest || conflict.Error.Code != commerceErrorPlanSKUCodeConflict || conflict.Error.Fields["code"] == "" {
		t.Fatalf("conflict contract: %d %s", w.Code, w.Body.String())
	}
	const failCallback = "commerce-http-fail-audit"
	if err := f.h.db.Callback().Create().Before("gorm:create").Register(failCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_logs" {
			tx.AddError(errors.New("private SQL diagnostic"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	request.Code = "http-rollback"
	failedBody, _ := json.Marshal(request)
	w = httptest.NewRecorder()
	f.h.PlanSKUCreateCommerceHandler(w, announcementRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/plans/%d/skus", plan.ID), token, string(failedBody)))
	f.h.db.Callback().Create().Remove(failCallback)
	if err := json.Unmarshal(w.Body.Bytes(), &conflict); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusInternalServerError || conflict.Error.Code != commerceErrorPersistenceFailed || strings.Contains(w.Body.String(), "private SQL diagnostic") {
		t.Fatalf("persistence failure: %d %s", w.Code, w.Body.String())
	}
	request.Code = ""
	body, _ = json.Marshal(request)
	w = httptest.NewRecorder()
	f.h.PlanSKUCreateCommerceHandler(w, announcementRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/plans/%d/skus", plan.ID), token, string(body)))
	var failure struct {
		Error struct{ Fields map[string]string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusBadRequest || failure.Error.Fields["code"] == "" {
		t.Fatalf("validation contract: %d %s", w.Code, w.Body.String())
	}
}
