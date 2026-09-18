package platformstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

type MigrationTargets struct{}

func (MigrationTargets) Check(ctx context.Context, target platform.MigrationTarget) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := datastore.ValidateDataSource(target.TargetDriver, target.TargetDataSource, false); err != nil {
		return platform.ErrMigrationInvalid
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
	if err := pool.PingContext(ctx); err != nil {
		return migrationCheckError(platform.ErrMigrationTargetConnection, err)
	}
	if err := EnsureMigrationTargetEmpty(db.WithContext(ctx)); err != nil {
		return migrationCheckError(platform.ErrMigrationTargetOccupied, err)
	}
	return nil
}

func OpenMigrationTarget(ctx context.Context, driver, dataSource string) (*gorm.DB, error) {
	connect, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return datastore.OpenWithDriverContext(connect, driver, dataSource, datastore.PoolConfig{MaxOpenConnections: 1, MaxIdleConnections: 1})
}
func EnsureMigrationTargetEmpty(db *gorm.DB) error {
	tables, err := datastore.MigrationTables(db)
	if err != nil {
		return err
	}
	present, err := db.Migrator().GetTables()
	if err != nil {
		return err
	}
	exists := map[string]bool{}
	for _, table := range present {
		exists[table] = true
	}
	for _, table := range tables {
		if !exists[table] {
			continue
		}
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			return fmt.Errorf("inspect target table %s: %w", table, err)
		}
		if count > 0 {
			return fmt.Errorf("target database is not empty: %s contains %d rows", table, count)
		}
	}
	return nil
}

// Keep cancellation classification without exposing raw driver/DSN errors.
func migrationCheckError(kind, cause error) error {
	if errors.Is(cause, context.Canceled) {
		return errors.Join(kind, context.Canceled)
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		return errors.Join(kind, context.DeadlineExceeded)
	}
	return kind
}
