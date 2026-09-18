package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

type migrationProbeStub struct {
	snapshot func(context.Context) (int, error)
}

func (s migrationProbeStub) Snapshot(ctx context.Context) (int, error) { return s.snapshot(ctx) }
func (migrationProbeStub) Describe(platform.MigrationTarget) string    { return "redacted target" }

func TestMigrationInspectionPermissionsAndSafeProjection(t *testing.T) {
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
			admin := model.User{Email: "migration-inspection@example.test", Password: "unused", Status: "active", IsAdmin: true}
			if err := h.db.Create(&admin).Error; err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			inspection := h.services.DatabaseMigrationInspection()
			if _, err := inspection.Status(ctx, 0); !errors.Is(err, platform.ErrMaintenancePermission) {
				t.Fatal("anonymous status", err)
			}
			if status, err := inspection.Status(ctx, admin.ID); err != nil || status.Task != nil || status.SourceDriver != engine {
				t.Fatal("initial status", status, err)
			}
			target := platform.MigrationTarget{TargetDriver: "mysql", TargetDataSource: "unused"}
			if engine == "mysql" {
				target.TargetDriver = "sqlite"
			}
			targetCalls, sourceCalls := 0, 0
			inspection.Targets = migrationTargetCheck(func(context.Context, platform.MigrationTarget) error { targetCalls++; return nil })
			inspection.Source = migrationProbeStub{snapshot: func(context.Context) (int, error) { sourceCalls++; return 3, nil }}
			if err := h.db.Model(&admin).Update("is_admin", false).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := inspection.Status(ctx, admin.ID); !errors.Is(err, platform.ErrMaintenancePermission) {
				t.Fatal("revoked status", err)
			}
			if _, err := inspection.Preflight(ctx, admin.ID, target); !errors.Is(err, platform.ErrMaintenancePermission) || targetCalls != 0 || sourceCalls != 0 {
				t.Fatal("revoked preflight probed databases", err)
			}
			if err := h.db.Model(&admin).Update("is_admin", true).Error; err != nil {
				t.Fatal(err)
			}
			inspection.Targets = migrationTargetCheck(func(context.Context, platform.MigrationTarget) error {
				return h.db.Model(&admin).Update("is_admin", false).Error
			})
			if _, err := inspection.Preflight(ctx, admin.ID, target); !errors.Is(err, platform.ErrMaintenancePermission) || sourceCalls != 0 {
				t.Fatal("revocation during target check reached source lock", err)
			}
			if err := h.db.Model(&admin).Update("is_admin", true).Error; err != nil {
				t.Fatal(err)
			}
			inspection.Targets = migrationTargetCheck(func(context.Context, platform.MigrationTarget) error { return nil })
			inspection.Source = migrationProbeStub{snapshot: func(context.Context) (int, error) { return 3, h.db.Model(&admin).Update("is_admin", false).Error }}
			if _, err := inspection.Preflight(ctx, admin.ID, target); !errors.Is(err, platform.ErrMaintenancePermission) {
				t.Fatal("revocation during source probe returned readiness", err)
			}
			if err := h.db.Model(&admin).Update("is_admin", true).Error; err != nil {
				t.Fatal(err)
			}
			task := model.Task{Type: "database_migration", Scope: `{"scope":"sensitive-scope"}`, Content: `{"ciphertext":"protected-ciphertext"}`, Status: 2, Total: 7, Current: 7}
			if err := h.db.Create(&task).Error; err != nil {
				t.Fatal(err)
			}
			if err := h.db.Model(&model.SystemConfig{}).Where("config_key = ?", "maintenance_enabled").Update("value", "true").Error; err != nil {
				t.Fatal(err)
			}
			if err := h.db.Model(&model.SystemConfig{}).Where("config_key = ?", "maintenance_task_id").Update("value", task.ID).Error; err != nil {
				t.Fatal(err)
			}
			payload, _ := json.Marshal(map[string]any{"revision": "1", "task_id": task.ID, "actor_id": admin.ID})
			run, err := jobstore.New(h.db).Submit(ctx, jobs.Submission{Owner: "system", Handler: "database_migration", Key: "database_migration:1", Resource: jobs.MaintenanceResource, Payload: string(payload)})
			if err != nil {
				t.Fatal(err)
			}
			if err := h.db.Model(&jobstore.Record{}).Where("id = ?", run.ID).Update("state", jobs.Succeeded).Error; err != nil {
				t.Fatal(err)
			}
			status, err := inspection.Status(ctx, admin.ID)
			if err != nil || status.Task == nil || status.Task.RunID != run.ID || status.NextStep == "" || !status.Maintenance.MigrationCutoverPending {
				t.Fatal("missing status correlation/cutover", status, err)
			}
			encoded, _ := json.Marshal(status)
			if strings.Contains(string(encoded), "sensitive-scope") || strings.Contains(string(encoded), "protected-ciphertext") || status.Task.Content != "" || status.Task.Scope != "" {
				t.Fatal("status exposed stored payload")
			}
			token, _, err := h.issueToken(authClaims{UserID: admin.ID, Email: admin.Email, IsAdmin: true})
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			h.AdminDatabaseMigrationStatusHandler(w, announcementRequest(http.MethodGet, "/api/v1/admin/database-migrations/status", token, ""))
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), run.ID) || strings.Contains(w.Body.String(), "protected-ciphertext") {
				t.Fatal("HTTP status projection", w.Code, w.Body.String())
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := inspection.Status(canceled, admin.ID); err == nil {
				t.Fatal("canceled read returned state")
			}
		})
	}
}

func TestMySQLMigrationPreflightUsesSingleSourceConnection(t *testing.T) {
	h, _ := newMySQLPublishHandlers(t)
	pool, _ := h.db.DB()
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	if err := h.ReconcileSystemConfigDefaults(); err != nil {
		t.Fatal(err)
	}
	admin := model.User{Email: "migration-probe@example.test", Password: "unused", Status: "active", IsAdmin: true}
	if err := h.db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	target := platform.MigrationTarget{TargetDriver: "sqlite", TargetDataSource: filepath.Join(t.TempDir(), "destination.db")}
	inspection := h.services.DatabaseMigrationInspection()
	result, err := inspection.Preflight(context.Background(), admin.ID, target)
	if err != nil || !result.Ready || result.Tables == 0 || result.SourceDriver != "mysql" || result.TargetDriver != "sqlite" {
		t.Fatal("preflight", result, err)
	}
	var count int64
	if err := h.db.Model(&model.Task{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("preflight created tasks", count, err)
	}
	if err := h.db.Model(&jobstore.Record{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("preflight created runs", count, err)
	}
	// A successful source probe releases its lock and returns the connection.
	if err := h.db.Model(&admin).Update("email", "migration-probe-after@example.test").Error; err != nil {
		t.Fatal("source remained locked", err)
	}
}

func TestMySQLMigrationPreflightChecksEmptyTargetAndRedactsPassword(t *testing.T) {
	target, dsn := newMySQLPublishHandlers(t)
	if err := datastore.ClearCopyDestination(target.db); err != nil {
		t.Fatal(err)
	}
	source, _ := newAnnouncementTestHandlers(t)
	if err := source.ReconcileSystemConfigDefaults(); err != nil {
		t.Fatal(err)
	}
	admin := model.User{Email: "source-probe@example.test", Password: "unused", Status: "active", IsAdmin: true}
	if err := source.db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	input := platform.MigrationTarget{TargetDriver: "mysql", TargetDataSource: dsn}
	inspection := source.services.DatabaseMigrationInspection()
	result, err := inspection.Preflight(context.Background(), admin.ID, input)
	if err != nil || !result.Ready || result.Tables == 0 {
		t.Fatal("empty MySQL target preflight failed", err)
	}
	config, err := mysqlconfig.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if config.Passwd == "" || strings.Contains(result.Target, config.Passwd) || !strings.Contains(result.Target, "***") {
		t.Fatal("preflight target description exposed credentials")
	}
	occupied := model.User{Email: "existing-target@example.test", Password: "unused", Status: "active"}
	if err := target.db.Create(&occupied).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := inspection.Preflight(context.Background(), admin.ID, input); !errors.Is(err, platform.ErrMigrationTargetOccupied) {
		t.Fatal("occupied target accepted", err)
	}
	var count int64
	if err := target.db.Model(&model.User{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("preflight modified occupied target", err)
	}
	if err := source.db.Model(&model.Task{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("preflight submitted migration", err)
	}
}
