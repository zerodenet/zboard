package application

import (
	"context"
	"errors"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ValidatePreparedProjectionDatabase is a read-only precondition for a future
// dedicated projection process. It does not construct Services, start a worker,
// open a spool or HTTP listener, or invoke preparation/default/secret migration.
// Success proves dependencies at inspection time only; WorkPaused must still
// guard every later claim. Enabled maintenance is valid prepared data, not a
// grant to execute work. Concurrent deployment DDL requires workers to stop.
func ValidatePreparedProjectionDatabase(ctx context.Context, db *gorm.DB) error {
	if err := datastore.ValidatePreparedProjectionSchema(ctx, db); err != nil {
		return err
	}
	// Reuse the single strict maintenance authority, including missing/invalid
	// source detection and legacy absent-task/reserved-ledger fallback. No cache,
	// source initialization or duplicate boolean/task-pointer parsing is used.
	if _, err := (platformstore.Maintenance{DB: db.Session(&gorm.Session{Logger: logger.Discard})}).Read(ctx); err != nil {
		stage := "maintenance authority unreadable"
		if errors.Is(err, platform.ErrMaintenanceIncomplete) {
			stage = "maintenance source incomplete or invalid"
		}
		return &datastore.ProjectionValidationError{Stage: stage, Kind: datastore.ErrProjectionNotPrepared, Cause: err}
	}
	return nil
}
