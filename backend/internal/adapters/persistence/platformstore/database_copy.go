package platformstore

import (
	"context"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"time"
)

// CopyDatabaseSnapshot runs after the platform's maintenance and empty-target
// guards. Source locking, destination defaults, data and copied-task completion
// share the owned source session and one destination transaction.
func CopyDatabaseSnapshot(ctx context.Context, source, target *gorm.DB, taskID uint) error {
	return copySnapshot(ctx, source, target, taskID, "")
}
func CopyMaintenanceDatabaseSnapshot(ctx context.Context, source, target *gorm.DB, taskID uint, runID string) error {
	if runID == "" {
		return fmt.Errorf("maintenance execution is required")
	}
	return copySnapshot(ctx, source, target, taskID, runID)
}
func copySnapshot(ctx context.Context, source, target *gorm.DB, taskID uint, runID string) error {
	if err := NormalizeMigrationSecrets(source.WithContext(ctx)); err != nil {
		return err
	}
	return datastore.WithMigrationSource(ctx, source, func(snapshot *gorm.DB) error {
		return datastore.WithCopyDestination(ctx, target, func(tx *gorm.DB) error {
			tables, err := datastore.MigrationTables(tx)
			if err != nil {
				return err
			}
			if err := datastore.ClearCopyDestinationRows(tx, tables); err != nil {
				return err
			}
			if err := datastore.CopyApplicationData(snapshot, tx); err != nil {
				return err
			}
			now := time.Now().UTC()
			result := tx.Model(&model.Task{}).Where("id = ? AND type = ?", taskID, "database_migration").Updates(map[string]interface{}{"status": int16(2), "current": gorm.Expr("total"), "errors": "", "content": platform.EncodeMigrationSecret(""), "finished_at": now})
			if result.Error != nil {
				return fmt.Errorf("finalize target migration task: %w", result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("copied migration task %d is missing", taskID)
			}
			if err := tx.Model(&model.TaskItem{}).Where("task_id = ?", taskID).Updates(map[string]interface{}{"status": int16(2), "error": "", "finished_at": now}).Error; err != nil {
				return fmt.Errorf("finalize target migration task item: %w", err)
			}
			if runID != "" {
				return finishCopiedMigrationRun(ctx, tx, taskID, runID)
			}
			return nil
		})
	})
}
