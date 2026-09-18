package application

import (
	"context"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

// PrepareDatabaseSchema is the complete schema boundary for startup,
// migrate-only and database-copy destinations. It starts no runtime or worker.
// MySQL DDL is not transactional; stop at the first error so operators can
// inspect and retry the existing idempotent migration/reconciliation steps.
func PrepareDatabaseSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	for _, step := range []struct {
		name string
		run  func(*gorm.DB) error
	}{
		{"versioned migrations", datastore.RunMigrations},
		{"commerce", datastore.ReconcileCommerceSchema},
		{"subscription access", datastore.ReconcileSubscriptionAccessSchema},
		{"event projections", datastore.ReconcileZeroEventSchema},
		{"traffic read models", datastore.ReconcileTrafficReadSchema},
		{"operations", datastore.ReconcileOperationsSchema},
		{"fair use telemetry", datastore.ReconcileFairUseTelemetrySchema},
	} {
		if err := step.run(db); err != nil {
			return fmt.Errorf("prepare database %s: %w", step.name, err)
		}
	}
	tables, err := datastore.MigrationTables(db)
	if err != nil {
		return err
	}
	for _, table := range tables {
		if !db.Migrator().HasTable(table) {
			return fmt.Errorf("prepared database is missing required table %s", table)
		}
	}
	return nil
}

// CopyDatabaseData is the platform infrastructure entry used after maintenance,
// source snapshot and empty-destination checks. It never starts a private worker.
func CopyDatabaseData(source, target *gorm.DB) error {
	return datastore.CopyApplicationData(source, target)
}

// ClearDatabaseCopyDestination is used only after the empty-target guard.
func ClearDatabaseCopyDestination(target *gorm.DB) error {
	return datastore.ClearCopyDestination(target)
}

func CopyDatabaseSnapshot(ctx context.Context, source, target *gorm.DB, taskID uint) error {
	return platformstore.CopyDatabaseSnapshot(ctx, source, target, taskID)
}

func (s *Services) DatabaseMigration(cipher platform.MigrationCipher) platform.Migration {
	driver := datastore.DriverMySQL
	if datastore.IsSQLite(s.Identity.db) {
		driver = datastore.DriverSQLite
	}
	return platform.Migration{SourceDriver: driver, Repository: migrationAdmissionRepository{MigrationRepository: platformstore.MigrationAdmission{DB: s.Identity.db}, invalidate: s.InvalidateMaintenance}, Targets: platformstore.MigrationTargets{}, Cipher: cipher}
}

func (s *Services) DatabaseMigrationInspection() platform.MigrationInspection {
	driver := datastore.DriverMySQL
	if datastore.IsSQLite(s.Identity.db) {
		driver = datastore.DriverSQLite
	}
	store := platformstore.MigrationInspection{DB: s.Identity.db}
	return platform.MigrationInspection{SourceDriver: driver, Repository: store, Admission: platformstore.MigrationAdmission{DB: s.Identity.db}, Targets: platformstore.MigrationTargets{}, Source: store}
}
