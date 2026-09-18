package platformstore

import (
	"context"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"gorm.io/gorm"
)

func finishCopiedMigrationRun(ctx context.Context, tx *gorm.DB, taskID uint, runID string) error {
	var row jobstore.Record
	if err := tx.Where("id = ? AND owner = ? AND handler = ? AND `key` = ? AND resource = ? AND state = ?", runID, "system", "database_migration", fmt.Sprintf("database_migration:%d", taskID), jobs.MaintenanceResource, jobs.Running).First(&row).Error; err != nil {
		return err
	}
	if row.ExpiresAt == nil {
		return jobs.ErrLeaseLost
	}
	return jobstore.New(tx).Finish(ctx, jobs.Claim{Run: jobs.Run{ID: row.ID}, Token: row.Token, Worker: row.Worker, ExpiresAt: *row.ExpiresAt}, jobs.Succeeded)
}
