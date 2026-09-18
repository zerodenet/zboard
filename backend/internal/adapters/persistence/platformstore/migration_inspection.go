package platformstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type MigrationInspection struct{ DB *gorm.DB }

func (s MigrationInspection) Read(ctx context.Context, actor uint) (out platform.MigrationStatusData, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := RequireMigrationAdministrator(tx, actor); err != nil {
			if errors.Is(err, jobs.ErrPermission) {
				return platform.ErrMaintenancePermission
			}
			return err
		}
		var task model.Task
		// Do not fetch protected task content or scope just to erase it afterwards.
		result := tx.Select("id", "type", "status", "errors", "total", "current", "idempotency_key", "priority", "scheduled_at", "started_at", "finished_at", "attempts", "max_attempts", "locked_by", "locked_until", "created_at", "updated_at").Where("type = ?", "database_migration").Order("id DESC").Limit(1).Find(&task)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			out.Task = &platform.MigrationTask{ID: task.ID, Type: task.Type, Status: task.Status, Errors: task.Errors, Total: task.Total, Current: task.Current, IdempotencyKey: task.IdempotencyKey, Priority: task.Priority, ScheduledAt: task.ScheduledAt, StartedAt: task.StartedAt, FinishedAt: task.FinishedAt, Attempts: task.Attempts, MaxAttempts: task.MaxAttempts, LockedBy: task.LockedBy, LockedUntil: task.LockedUntil, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt}
			var run jobstore.Record
			if err := tx.Select("id").Where("owner = ? AND handler = ? AND `key` = ?", "system", "database_migration", fmt.Sprintf("database_migration:%d", task.ID)).Limit(1).Find(&run).Error; err != nil {
				return err
			}
			out.Task.RunID = run.ID
		}
		var err error
		out.Maintenance, err = maintenanceData(tx, false)
		return err
	})
	if err != nil {
		return platform.MigrationStatusData{}, err
	}
	return out, nil
}
func (s MigrationInspection) Snapshot(ctx context.Context) (tables int, err error) {
	err = datastore.WithMigrationSource(ctx, s.DB, func(snapshot *gorm.DB) error {
		list, err := datastore.MigrationTables(snapshot)
		tables = len(list)
		return err
	})
	if err != nil {
		return 0, platform.ErrMigrationSourceSnapshot
	}
	return tables, nil
}
func (s MigrationInspection) Describe(target platform.MigrationTarget) string {
	return datastore.QuoteDataSource(target.TargetDriver, target.TargetDataSource)
}
