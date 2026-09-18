package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestPlatformMaintenanceAuthorityAndLegacySettings(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql"} {
		t.Run(engine, func(t *testing.T) {
			var h *handlers
			if engine == "mysql" {
				h, _ = newMySQLPublishHandlers(t)
			} else {
				h, _ = newAnnouncementTestHandlers(t)
			}
			if err := h.ReconcileSystemConfigDefaults(); err != nil {
				t.Fatal(err)
			}
			admin := model.User{Email: "platform-maintenance@example.test", Password: "unused", Status: "active", IsAdmin: true}
			if err := h.db.Create(&admin).Error; err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			revisions := func() map[string]uint64 {
				var configs []model.SystemConfig
				if err := h.db.Find(&configs).Error; err != nil {
					t.Fatal(err)
				}
				out := map[string]uint64{}
				for _, c := range configs {
					out[c.ConfigKey] = c.Revision
				}
				return out
			}
			in := platform.MaintenanceUpdate{Enabled: true, Title: "  维护  ", Message: "  等待  ", ExpectedRevisions: revisions()}
			if err := h.services.Maintenance.Update(ctx, 0, in); !errors.Is(err, platform.ErrMaintenancePermission) {
				t.Fatal("anonymous mutation", err)
			}
			if err := h.services.Maintenance.Update(ctx, admin.ID, in); err != nil {
				t.Fatal(err)
			}
			state, err := h.services.Maintenance.State(ctx)
			if err != nil || !state.Enabled || state.Title != "维护" || state.Message != "等待" {
				t.Fatal(state, err)
			}
			if err := h.services.Maintenance.Update(ctx, admin.ID, in); !errors.Is(err, platform.ErrMaintenanceRevision) {
				t.Fatal("stale update", err)
			}
			in.ExpectedRevisions = revisions()
			if err := h.db.Model(&admin).Update("is_admin", false).Error; err != nil {
				t.Fatal(err)
			}
			if err := h.services.Maintenance.Update(ctx, admin.ID, in); !errors.Is(err, platform.ErrMaintenancePermission) {
				t.Fatal("revoked admin", err)
			}
			if err := h.services.Maintenance.Patch(ctx, admin.ID, platform.MaintenanceSetting{Key: "maintenance_enabled", Value: "false"}); !errors.Is(err, platform.ErrMaintenancePermission) {
				t.Fatal("revoked legacy admin", err)
			}
			var audits int64
			if err := h.db.Model(&model.AuditLog{}).Where("user_id = ? AND action = ?", admin.ID, "maintenance.update").Count(&audits).Error; err != nil || audits != 1 {
				t.Fatal("failed operations audited as success", audits, err)
			}
			if err := h.db.Model(&admin).Update("is_admin", true).Error; err != nil {
				t.Fatal(err)
			}
			token, _, err := h.issueToken(authClaims{UserID: admin.ID, Email: admin.Email, IsAdmin: true})
			if err != nil {
				t.Fatal(err)
			}
			run, err := jobstore.New(h.db).Submit(ctx, jobs.Submission{Owner: "system", Handler: "database_migration", Key: "maintenance-test", Resource: jobs.MaintenanceResource, Payload: "{}"})
			if err != nil {
				t.Fatal(err)
			}
			if err := h.db.Model(&jobstore.Record{}).Where("id = ?", run.ID).Update("state", jobs.Unknown).Error; err != nil {
				t.Fatal(err)
			}
			update := func(key string, value any, expected uint64) *httptest.ResponseRecorder {
				payload, _ := json.Marshal(map[string]any{"value": value, "expected_revision": expected})
				w := httptest.NewRecorder()
				h.AdminSystemConfigUpdateHandler(w, announcementRequest(http.MethodPut, "/api/v1/admin/system-configs/"+key, token, string(payload)))
				return w
			}
			before := revisions()
			if w := update("maintenance_enabled", false, before["maintenance_enabled"]); w.Code != http.StatusBadRequest {
				t.Fatal("legacy route bypassed unknown", w.Code, w.Body.String())
			}
			if w := update("maintenance_title", "检查目标库", before["maintenance_title"]); w.Code != http.StatusOK {
				t.Fatal("copy update blocked", w.Code, w.Body.String())
			}
			after := revisions()
			if after["maintenance_enabled"] != before["maintenance_enabled"] || after["maintenance_title"] != before["maintenance_title"]+1 {
				t.Fatal("unexpected revision mutation", before, after)
			}
			if w := update("maintenance_title", "stale", before["maintenance_title"]); w.Code != http.StatusConflict {
				t.Fatal("legacy stale write accepted", w.Code, w.Body.String())
			}
			if state, err := h.services.Maintenance.State(ctx); err != nil || !state.Enabled || !state.MigrationInProgress {
				t.Fatal("unknown state not exposed", state, err)
			}
			auditFailure := errors.New("audit unavailable")
			callback := "maintenance-test-audit-failure"
			if err := h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "audit_logs" {
					tx.AddError(auditFailure)
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { h.db.Callback().Create().Remove(callback) })
			beforeFailure := revisions()
			err = h.services.Maintenance.Update(ctx, admin.ID, platform.MaintenanceUpdate{Enabled: true, Title: "uncommitted", Message: "uncommitted", ExpectedRevisions: beforeFailure})
			if !errors.Is(err, auditFailure) {
				t.Fatal("audit failure not propagated", err)
			}
			afterFailure := revisions()
			for key, revision := range beforeFailure {
				if afterFailure[key] != revision {
					t.Fatal("audit failure committed revision", key)
				}
			}
			if state, err := h.services.Maintenance.State(ctx); err != nil || state.Title != "检查目标库" {
				t.Fatal("audit failure committed settings", state, err)
			}

		})
	}
}
