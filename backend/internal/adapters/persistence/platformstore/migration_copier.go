package platformstore

import (
	"context"
	"fmt"

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
	db, err := OpenMigrationTarget(ctx, target.TargetDriver, target.TargetDataSource)
	if err != nil {
		return fmt.Errorf("open target: %w", err)
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := EnsureMigrationTargetEmpty(db.WithContext(ctx)); err != nil {
		return err
	}
	if err := s.PrepareSchema(db.WithContext(ctx)); err != nil {
		return fmt.Errorf("prepare target schema: %w", err)
	}
	return CopyMaintenanceDatabaseSnapshot(ctx, s.DB, db, taskID, runID)
}
