package main

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/application"
	cfgpkg "github.com/zerodenet/zboard/backend/internal/config"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

func validateDatabaseModes(migrate, rollback, check bool) error {
	if migrate && rollback {
		return errors.New("migrate-only and prepare-hourly-rollback are mutually exclusive")
	}
	if check && (migrate || rollback) {
		return errors.New("check-projection-database, migrate-only and prepare-hourly-rollback are mutually exclusive")
	}
	return nil
}
func checkProjectionDatabase(ctx context.Context, c cfgpkg.Config) (err error) {
	db, err := datastore.OpenProjectionCheckDatabase(ctx, c.DatabaseDriver, c.DataSource, datastore.PoolConfig{
		MaxOpenConnections: c.DatabaseMaxOpenConnections, MaxIdleConnections: c.DatabaseMaxIdleConnections, ConnectionLifetime: time.Duration(c.DatabaseConnectionMaxLifetimeSecs) * time.Second,
	})
	if err != nil {
		return &datastore.ProjectionValidationError{Stage: "connection initialization", Kind: datastore.ErrProjectionNotPrepared, Cause: err}
	}
	pool, err := db.DB()
	if err != nil {
		return &datastore.ProjectionValidationError{Stage: "connection pool", Kind: datastore.ErrProjectionNotPrepared, Cause: err}
	}
	defer func() {
		if closeErr := pool.Close(); closeErr != nil {
			err = errors.Join(err, &datastore.ProjectionValidationError{Stage: "connection close", Kind: datastore.ErrProjectionNotPrepared, Cause: closeErr})
		}
	}()
	if datastore.IsSQLite(db) {
		return application.ValidatePreparedProjectionDatabase(ctx, db)
	}
	// No preparation, credentials, defaults, bootstrap, Services, HTTP or spool
	// is reachable through this branch. The engine rejects attempted DML/DDL.
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return application.ValidatePreparedProjectionDatabase(ctx, tx) }, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err == nil {
		return nil
	}
	var classified *datastore.ProjectionValidationError
	if errors.As(err, &classified) {
		return err
	}
	return &datastore.ProjectionValidationError{Stage: "read-only transaction", Kind: datastore.ErrProjectionNotPrepared, Cause: err}
}
