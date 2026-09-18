package handler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type migrationTargetCheck func(context.Context, platform.MigrationTarget) error

func (f migrationTargetCheck) Check(ctx context.Context, target platform.MigrationTarget) error {
	return f(ctx, target)
}

func TestMigrationCapabilityAdmissionIsAtomicAndRechecksAuthority(t *testing.T) {
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
			admin := model.User{Email: "admission-admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
			if err := h.db.Create(&admin).Error; err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			service := h.services.DatabaseMigration(h.credentialCipher)
			driver := "mysql"
			if engine == "mysql" {
				driver = "sqlite"
			}
			input := platform.MigrationStart{Target: platform.MigrationTarget{TargetDriver: driver, TargetDataSource: "private-target-secret"}, Confirm: true}
			checks := 0
			service.Targets = migrationTargetCheck(func(context.Context, platform.MigrationTarget) error { checks++; return nil })
			unconfirmed := input
			unconfirmed.Confirm = false
			if _, err := service.Start(ctx, admin.ID, unconfirmed); !errors.Is(err, platform.ErrMigrationConfirmation) {
				t.Fatal("confirmation boundary", err)
			}
			invalid := input
			invalid.Target.TargetDriver = engine
			if _, err := service.Start(ctx, admin.ID, invalid); !errors.Is(err, platform.ErrMigrationInvalid) {
				t.Fatal("same-driver migration accepted", err)
			}
			if checks != 0 {
				t.Fatal("invalid request probed target", checks)
			}

			assertEmpty := func() {
				for _, row := range []any{&model.Task{}, &model.TaskItem{}, &jobstore.Record{}} {
					var count int64
					if err := h.db.Model(row).Count(&count).Error; err != nil || count != 0 {
						t.Fatalf("admission left %T rows: %d %v", row, count, err)
					}
				}
				var config model.SystemConfig
				if err := h.db.Where("config_key = ?", "maintenance_enabled").First(&config).Error; err != nil || config.Value != "false" || config.Revision != 1 {
					t.Fatal("partial maintenance mutation", config.Value, config.Revision, err)
				}
			}
			if _, err := service.Start(ctx, 0, input); !errors.Is(err, platform.ErrMaintenancePermission) {
				t.Fatal(err)
			}
			if err := h.db.Model(&admin).Update("is_admin", false).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := service.Start(ctx, admin.ID, input); !errors.Is(err, platform.ErrMaintenancePermission) {
				t.Fatal(err)
			}
			if checks != 0 {
				t.Fatal("unauthorized target connection", checks)
			}
			if err := h.db.Model(&admin).Update("is_admin", true).Error; err != nil {
				t.Fatal(err)
			}
			service.Targets = migrationTargetCheck(func(context.Context, platform.MigrationTarget) error {
				return h.db.Model(&admin).Update("is_admin", false).Error
			})
			if _, err := service.Start(ctx, admin.ID, input); !errors.Is(err, platform.ErrMaintenancePermission) {
				t.Fatal("authority not rechecked", err)
			}
			assertEmpty()
			if err := h.db.Model(&admin).Update("is_admin", true).Error; err != nil {
				t.Fatal(err)
			}
			service.Targets = migrationTargetCheck(func(context.Context, platform.MigrationTarget) error { return nil })

			service.Targets = migrationTargetCheck(func(context.Context, platform.MigrationTarget) error {
				run, err := jobstore.New(h.db).Submit(ctx, jobs.Submission{Owner: "system", Key: "during-preflight", Handler: "test", Resource: "test", Payload: "{}"})
				if err != nil {
					return err
				}
				return h.db.Model(&jobstore.Record{}).Where("id = ?", run.ID).Update("state", jobs.Unknown).Error
			})
			if _, err := service.Start(ctx, admin.ID, input); !errors.Is(err, platform.ErrMigrationBusy) {
				t.Fatal("new unknown work not rechecked", err)
			}
			if err := h.db.Where("`key` = ?", "during-preflight").Delete(&jobstore.Record{}).Error; err != nil {
				t.Fatal(err)
			}
			assertEmpty()
			service.Targets = migrationTargetCheck(func(context.Context, platform.MigrationTarget) error { return nil })
			auditFailure := errors.New("audit unavailable")
			callback := "migration-admission-audit-failure"
			if err := h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "audit_logs" {
					tx.AddError(auditFailure)
				}
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Start(ctx, admin.ID, input); !errors.Is(err, auditFailure) {
				t.Fatal("audit failure hidden", err)
			}
			assertEmpty()
			if err := h.db.Callback().Create().Remove(callback); err != nil {
				t.Fatal(err)
			}
			var missing model.SystemConfig
			if err := h.db.Where("config_key = ?", "maintenance_task_id").First(&missing).Error; err != nil {
				t.Fatal(err)
			}
			if err := h.db.Delete(&missing).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := service.Start(ctx, admin.ID, input); !errors.Is(err, platform.ErrMaintenanceIncomplete) {
				t.Fatal("missing maintenance accepted", err)
			}
			assertEmpty()
			if err := h.db.Create(&missing).Error; err != nil {
				t.Fatal(err)
			}
			accepted, err := service.Start(ctx, admin.ID, input)
			if err != nil {
				t.Fatal(err)
			}
			var task model.Task
			if err := h.db.First(&task, accepted.TaskID).Error; err != nil {
				t.Fatal(err)
			}
			ciphertext, err := platform.DecodeMigrationSecret(task.Content)
			if err != nil || ciphertext == "" {
				t.Fatal("missing sealed target", err)
			}
			var run jobstore.Record
			if err := h.db.First(&run, "id = ?", accepted.RunID).Error; err != nil {
				t.Fatal(err)
			}
			if strings.Contains(run.Payload, "private-target-secret") || strings.Contains(run.Payload, ciphertext) || strings.Contains(task.Content, "private-target-secret") {
				t.Fatal("migration secret leaked into public job payload")
			}
			if run.Resource != jobs.MaintenanceResource || accepted.IdempotencyKey == "" {
				t.Fatal("wrong admission", run, accepted)
			}
			if _, err := service.Start(ctx, admin.ID, input); !errors.Is(err, jobs.ErrConflict) {
				t.Fatal("duplicate live migration admitted", err)
			}
			// A verified failed migration may be followed by a distinct intent without
			// colliding with the old empty unique task key.
			if err := h.db.Model(&task).Updates(map[string]any{"status": 3, "content": "{}"}).Error; err != nil {
				t.Fatal(err)
			}
			if err := h.db.Model(&run).Update("state", jobs.Failed).Error; err != nil {
				t.Fatal(err)
			}
			next, err := service.Start(ctx, admin.ID, input)
			if err != nil || next.TaskID == accepted.TaskID || next.RunID == accepted.RunID || next.IdempotencyKey == accepted.IdempotencyKey {
				t.Fatal("subsequent intent rejected", next, err)
			}
		})
	}
}
