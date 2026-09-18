package platformstore

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"time"
)

// RecoverLegacy only terminates interrupted pre-ledger migrations. Linked
// public intents keep their durable state and maintenance remains enabled.
func (s MigrationExecution) RecoverLegacy(ctx context.Context) error {
	now := time.Now().UTC()
	return jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		var taskIDs []uint
		linked := "'database_migration:' || CAST(tasks.id AS TEXT)"
		if !datastore.IsSQLite(tx) {
			linked = "CONCAT('database_migration:', tasks.id)"
		}
		if err := tx.Model(&model.Task{}).
			Where("NOT EXISTS (SELECT 1 FROM job_runs WHERE owner = 'system' AND handler = 'database_migration' AND job_runs.`key` = "+linked+")").
			Where("type = ? AND status IN ?", "database_migration", []int16{0, 1}).
			Pluck("id", &taskIDs).Error; err != nil {
			return err
		}
		if len(taskIDs) == 0 {
			return nil
		}
		message := "database migration was interrupted by service restart; inspect the target and run preflight again"
		if err := tx.Model(&model.Task{}).Where("id IN ?", taskIDs).Updates(map[string]interface{}{
			"status": int16(3), "errors": message, "finished_at": now, "content": platform.EncodeMigrationSecret(""),
		}).Error; err != nil {
			return err
		}
		return tx.Model(&model.TaskItem{}).Where("task_id IN ? AND status IN ?", taskIDs, []int16{0, 1}).Updates(map[string]interface{}{
			"status": int16(3), "error": message, "finished_at": now,
		}).Error
	})
}
