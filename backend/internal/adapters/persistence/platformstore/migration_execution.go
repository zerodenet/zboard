package platformstore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MigrationExecution struct{ DB *gorm.DB }

func (s MigrationExecution) Begin(ctx context.Context, taskID uint, run jobs.Run) (payload string, err error) {
	err = jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		var record jobstore.Record
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND state = ? AND expires_at > ?", run.ID, jobs.Running, time.Now().UTC()).Limit(1).Find(&record)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 || record.Owner != run.Owner || record.Handler != run.Handler || record.Key != run.Key || record.Resource != run.Resource || record.Payload != run.Payload {
			return jobs.ErrLeaseLost
		}
		now := time.Now().UTC()
		result = tx.Model(&model.Task{}).Where("id = ? AND type = ? AND status = ?", taskID, "database_migration", 0).Updates(map[string]any{"status": int16(1), "started_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return jobs.ErrConflict
		}
		if err := tx.Model(&model.TaskItem{}).Where("task_id = ?", taskID).Updates(map[string]any{"status": int16(1), "started_at": now}).Error; err != nil {
			return err
		}
		var task model.Task
		if err := tx.Select("id", "content").First(&task, taskID).Error; err != nil {
			return err
		}
		payload = task.Content
		return nil
	})
	return payload, err
}
func (s MigrationExecution) Authorize(ctx context.Context, actor uint) error {
	return RequireMigrationAdministrator(s.DB.WithContext(ctx), actor)
}

func (s MigrationExecution) Finish(ctx context.Context, taskID uint, migrationErr error) error {
	status := int16(2)
	errorText := ""
	current := any(gorm.Expr("total"))
	if migrationErr != nil {
		status = 3
		errorText = migrationErr.Error()
		current = gorm.Expr("current")
	}
	now := time.Now().UTC()
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.Task{}).Where("id = ? AND type = ? AND status = ?", taskID, "database_migration", 1).Updates(map[string]any{"status": status, "current": current, "errors": errorText, "finished_at": now, "content": platform.EncodeMigrationSecret("")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return jobs.ErrLeaseLost
		}
		return tx.Model(&model.TaskItem{}).Where("task_id = ?", taskID).Updates(map[string]any{"status": status, "error": errorText, "finished_at": now}).Error
	})
}
