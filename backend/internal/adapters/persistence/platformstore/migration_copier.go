package platformstore

import (
	"context"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"gorm.io/gorm"
)

// PrepareSchema is supplied by application so the persistence adapter does not
// depend on the composition root or start a runtime during destination setup.
type MigrationCopier struct {
	DB            *gorm.DB
	PrepareSchema func(*gorm.DB) error
}

func (s MigrationCopier) Copy(ctx context.Context, target platform.MigrationTarget, taskID uint, runID string) error {
	if err := MigrationSourceQuiescent(s.DB.WithContext(ctx), runID); err != nil {
		return err
	}
	if _, err := migrationTaskIdentity(s.DB.WithContext(ctx), taskID, runID, true); err != nil {
		return err
	}
	db, err := OpenMigrationTarget(ctx, target.TargetDriver, target.TargetDataSource)
	if err != nil {
		return migrationCheckError(platform.ErrMigrationTargetConnection, err)
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := ensureMigrationTopologySupported(s.DB.WithContext(ctx), db.WithContext(ctx)); err != nil {
		return err
	}
	receipt, err := prepareMigrationDestination(ctx, s.DB, db, taskID, runID, s.PrepareSchema)
	if err != nil {
		return err
	}
	return copyPreparedMigrationSnapshot(ctx, s.DB, db, taskID, runID, receipt)
}
