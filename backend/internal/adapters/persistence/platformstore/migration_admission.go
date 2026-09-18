package platformstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type MigrationAdmission struct{ DB *gorm.DB }

func (s MigrationAdmission) Check(ctx context.Context, actor uint) error {
	return jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error { return migrationAdmissionCheck(tx, actor) })
}
func migrationAdmissionCheck(tx *gorm.DB, actor uint) error {
	if err := RequireMigrationAdministrator(tx, actor); err != nil {
		if errors.Is(err, jobs.ErrPermission) {
			return platform.ErrMaintenancePermission
		}
		return err
	}
	var active int64
	if err := tx.Model(&model.Task{}).Where("type = ? AND status IN ?", "database_migration", []int16{0, 1}).Count(&active).Error; err != nil {
		return err
	}
	if active > 0 {
		return jobs.ErrConflict
	}
	return MigrationSourceQuiescent(tx, "")
}
func (s MigrationAdmission) Admit(ctx context.Context, actor uint, driver, ciphertext string) (out platform.MigrationAccepted, err error) {
	if ciphertext == "" || (driver != "mysql" && driver != "sqlite") {
		return out, platform.ErrMigrationInvalid
	}
	err = jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		if err := migrationAdmissionCheck(tx, actor); err != nil {
			return err
		}
		tables, err := datastore.MigrationTables(tx)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		task := model.Task{IdempotencyKey: "database_migration:" + uuid.NewString(), Type: "database_migration", Scope: "{}", Content: platform.EncodeMigrationSecret(ciphertext), Status: 0, Total: int64(len(tables)), MaxAttempts: 1, Priority: 100, ScheduledAt: &now}
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.TaskItem{TaskID: task.ID, TargetType: "database", TargetID: driver, Status: 0, Payload: "{}"}).Error; err != nil {
			return err
		}
		for _, item := range []struct{ key, value string }{{"maintenance_enabled", "true"}, {"maintenance_task_id", fmt.Sprint(task.ID)}} {
			result := tx.Model(&model.SystemConfig{}).Where("config_key = ?", item.key).Updates(map[string]any{"value": item.value, "revision": gorm.Expr("revision + 1"), "updated_at": now})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return platform.ErrMaintenanceIncomplete
			}
		}
		payload, _ := json.Marshal(map[string]any{"revision": "1", "task_id": task.ID, "actor_id": actor})
		run, err := jobstore.New(tx).Submit(ctx, jobs.Submission{Owner: "system", Key: fmt.Sprintf("database_migration:%d", task.ID), Handler: "database_migration", Resource: jobs.MaintenanceResource, Timeout: time.Hour, Payload: string(payload)})
		if err != nil {
			return err
		}
		var user model.User
		if err := tx.Select("id", "email").First(&user, actor).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "database.migration.start", Target: fmt.Sprintf("task:%d", task.ID), Detail: "target_driver=" + driver}).Error; err != nil {
			return err
		}
		out = platform.MigrationAccepted{IdempotencyKey: task.IdempotencyKey, TaskID: task.ID, RunID: run.ID, Total: task.Total, Status: task.Status, Priority: task.Priority, MaxAttempts: task.MaxAttempts, ScheduledAt: task.ScheduledAt, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt}
		return nil
	})
	if err != nil {
		return platform.MigrationAccepted{}, err
	}
	return out, nil
}
