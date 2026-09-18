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

func TestCommercePlanUpdateRevisionAndAtomicity(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql"} {
		t.Run(engine, func(t *testing.T) {
			var h *handlers
			if engine == "mysql" {
				h, _ = newMySQLPublishHandlers(t)
			} else {
				h, _ = newAnnouncementTestHandlers(t)
			}
			actor := model.User{Email: "plan-editor@example.test", Password: "unused", Status: "active", IsAdmin: true}
			if err := h.db.Create(&actor).Error; err != nil {
				t.Fatal(err)
			}
			group := model.NodeGroup{Name: "Plans", Code: "plan-editor", IsEnabled: true}
			if err := h.db.Create(&group).Error; err != nil {
				t.Fatal(err)
			}
			plan := model.Plan{Name: "Original", Slug: "plan-editor", NodeGroupID: group.ID, IsActive: true}
			if err := h.db.Create(&plan).Error; err != nil {
				t.Fatal(err)
			}
			skuRequest := validCommerceSKURequest()
			skuRequest.Code = "plan-editor"
			if _, err := h.services.SKUCreation.Create(context.Background(), actor.ID, plan.ID, skuRequest); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			name := " Updated "
			_, err := h.services.PlanUpdate.Update(ctx, actor.ID, plan.ID, commerce.PlanUpdateRequest{Name: &name})
			var revision *commerce.PlanRevisionError
			if !errors.As(err, &revision) || !revision.Required || revision.Current != plan.Revision {
				t.Fatalf("missing revision: %v", err)
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			for i := 0; i < 2; i++ {
				go func() {
					<-start
					_, err := h.services.PlanUpdate.Update(ctx, actor.ID, plan.ID, commerce.PlanUpdateRequest{Name: &name, ExpectedRevision: &plan.Revision})
					results <- err
				}()
			}
			close(start)
			success, conflict := 0, 0
			for i := 0; i < 2; i++ {
				err := <-results
				if err == nil {
					success++
				} else if errors.As(err, &revision) && !revision.Required && revision.Current == plan.Revision+1 {
					conflict++
				} else {
					t.Errorf("competing update: %v", err)
				}
			}
			if success != 1 || conflict != 1 {
				t.Fatalf("revision competition: %d %d", success, conflict)
			}
			if err := h.db.First(&plan, plan.ID).Error; err != nil {
				t.Fatal(err)
			}
			if plan.Name != "Updated" {
				t.Fatalf("name normalization: %q", plan.Name)
			}
			other := model.Plan{Name: "Other", Slug: "reserved-plan-slug", NodeGroupID: group.ID}
			if err := h.db.Create(&other).Error; err != nil {
				t.Fatal(err)
			}
			_, err = h.services.PlanUpdate.Update(ctx, actor.ID, plan.ID, commerce.PlanUpdateRequest{Slug: &other.Slug, ExpectedRevision: &plan.Revision})
			if !errors.Is(err, commerce.ErrPlanSlugConflict) {
				t.Fatalf("slug conflict: %v", err)
			}
			if engine == "mysql" {
				_, err := h.services.PlanUpdate.Update(ctx, actor.ID, plan.ID, commerce.PlanUpdateRequest{Name: &other.Name, ExpectedRevision: &plan.Revision})
				if !errors.Is(err, commerce.ErrPlanNameConflict) {
					t.Fatalf("legacy name update conflict: %v", err)
				}
			}
			const callback = "plan-update-audit-failure"
			if err := h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "audit_logs" {
					tx.AddError(errors.New("audit unavailable"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			newName := "must rollback"
			view, err := h.services.PlanUpdate.Update(ctx, actor.ID, plan.ID, commerce.PlanUpdateRequest{Name: &newName, ExpectedRevision: &plan.Revision})
			h.db.Callback().Create().Remove(callback)
			if err == nil || view.ID != 0 {
				t.Fatalf("rollback result: %+v %v", view, err)
			}
			var after model.Plan
			if err := h.db.First(&after, plan.ID).Error; err != nil {
				t.Fatal(err)
			}
			if after.Revision != plan.Revision || after.Name != plan.Name {
				t.Fatal("audit failure retained patch/revision")
			}
			zero := 0
			no := false
			empty := ""
			view, err = h.services.PlanUpdate.Update(ctx, actor.ID, plan.ID, commerce.PlanUpdateRequest{Summary: &empty, SpeedLimitMbps: &zero, IsRenewable: &no, ExpectedRevision: &plan.Revision})
			if err != nil {
				t.Fatal(err)
			}
			if view.IsRenewable || view.SpeedLimitMbps != 0 || view.Revision != plan.Revision+1 || len(view.SKUs) != 1 || view.CreatedAt.IsZero() {
				t.Fatalf("patch/view: %+v", view)
			}
			plan.Revision = view.Revision
			// Explicit publishing must validate the group's active protocol membership.
			yes := true
			_, err = h.services.PlanUpdate.Update(ctx, actor.ID, plan.ID, commerce.PlanUpdateRequest{IsActive: &yes, ExpectedRevision: &plan.Revision})
			var invalid *commerce.ValidationError
			if !errors.As(err, &invalid) || invalid.Fields["node_group_id"] == "" {
				t.Fatalf("group without endpoint: %v", err)
			}
			attachCommerceEndpoint(t, h, group.ID)
			view, err = h.services.PlanUpdate.Update(ctx, actor.ID, plan.ID, commerce.PlanUpdateRequest{IsActive: &yes, ExpectedRevision: &plan.Revision})
			if err != nil || !view.IsActive {
				t.Fatalf("publish with explicit membership: %+v %v", view, err)
			}
			plan.Revision = view.Revision
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := h.services.PlanUpdate.Update(canceled, actor.ID, plan.ID, commerce.PlanUpdateRequest{Name: &name, ExpectedRevision: &plan.Revision}); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled: %v", err)
			}
			if err := h.db.Model(&actor).Update("is_admin", false).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := h.services.PlanUpdate.Update(ctx, actor.ID, plan.ID, commerce.PlanUpdateRequest{Name: &name, ExpectedRevision: &plan.Revision}); !errors.Is(err, commerce.ErrPermission) {
				t.Fatalf("revoked: %v", err)
			}
		})
	}
}

func TestCommercePlanUpdateHTTPRevisionContract(t *testing.T) {
	f := newOrderFixture(t)
	path := fmt.Sprintf("/api/v1/admin/plans/%d", f.planRecord.ID)
	for _, endpoint := range []http.HandlerFunc{f.h.PlanUpdateHandler} {
		w := httptest.NewRecorder()
		endpoint(w, announcementRequest(http.MethodPut, path, f.token, `{"name":"Updated"}`))
		var response struct {
			Data struct {
				Current uint64 `json:"current_revision"`
			}
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusPreconditionRequired || response.Data.Current != f.planRecord.Revision {
			t.Fatalf("revision response: %d %s", w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	f.h.PlanUpdateHandler(w, announcementRequest(http.MethodPut, path, f.token, fmt.Sprintf(`{"name":"Updated","expected_revision":%d}`, f.planRecord.Revision)))
	var response struct{ Data commerce.Plan }
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || response.Data.Revision != f.planRecord.Revision+1 || response.Data.Name != "Updated" || len(response.Data.SKUs) != 1 {
		t.Fatalf("updated response: %d %s", w.Code, w.Body.String())
	}
}

func attachCommerceEndpoint(t *testing.T, h *handlers, groupID uint) {
	t.Helper()
	node := model.Node{Name: "Commerce endpoint", Address: "commerce.example.test", IsEnabled: true, Config: "{}"}
	if err := h.db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, Name: "Commerce", RuntimeKey: fmt.Sprintf("commerce-%d", groupID), Protocol: "trojan", Address: node.Address, Port: 443, PublicPort: 443, IsActive: true, ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]"}
	if err := h.db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Create(&model.NodeGroupEndpoint{NodeGroupID: groupID, ProtocolEndpointID: endpoint.ID}).Error; err != nil {
		t.Fatal(err)
	}
}
