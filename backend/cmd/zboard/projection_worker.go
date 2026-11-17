package main

import (
	"context"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/application"
	cfgpkg "github.com/zerodenet/zboard/backend/internal/config"
	"github.com/zerodenet/zboard/backend/internal/datastore"
)

func validateProjectionWorkerMode(worker, migrate, rollback, check bool, drain time.Duration) error {
	if !worker {
		return nil
	}
	if migrate || rollback || check {
		return errors.New("projection-worker cannot be combined with migrate-only, prepare-hourly-rollback or check-projection-database")
	}
	if drain < time.Second || drain > 5*time.Minute {
		return errors.New("projection-worker-drain-timeout must be between one second and five minutes")
	}
	return nil
}

func runProjectionWorker(ctx context.Context, c cfgpkg.Config, drainTimeout time.Duration) (err error) {
	// Reuse the noncreating, read-only connection before opening any writer.
	// This refuses missing SQLite paths without creating DB/WAL/defaults and
	// refuses incomplete MySQL preparation without attempting DDL or bootstrap.
	if err := checkProjectionDatabase(ctx, c); err != nil {
		return err
	}
	db, err := datastore.OpenWithDriver(c.DatabaseDriver, c.DataSource, datastore.PoolConfig{
		MaxOpenConnections: c.DatabaseMaxOpenConnections, MaxIdleConnections: c.DatabaseMaxIdleConnections,
		ConnectionLifetime: time.Duration(c.DatabaseConnectionMaxLifetimeSecs) * time.Second,
	})
	if err != nil {
		return errors.New("projection worker connection failed")
	}
	pool, err := db.DB()
	if err != nil {
		return errors.New("projection worker pool failed")
	}
	defer func() { err = errors.Join(err, pool.Close()) }()
	// Recheck physical preparation after the writer connects. Deployment must
	// still stop all writers before DDL; this does not claim to fence live DDL.
	return application.RunProjectionWorker(ctx, db, drainTimeout)
}
