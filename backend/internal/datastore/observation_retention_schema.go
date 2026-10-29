package datastore

import (
	"fmt"
	"slices"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const observationRetentionSchemaVersion = "0056_observation_retention_index.up.sql"
const observationRetentionIndex = "idx_fair_use_event_retention"

// A LIMIT on candidate output cannot bound a scan across all subscriptions.
// Install the global time/PK index before background maintenance can start.
// This is additive derived-schema recovery; it never deletes diagnostic rows.
func reconcileObservationRetentionSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	// MySQL historically installs these operational tables separately at startup.
	// Reuse that compatible creation path before the new index migration. SQLite
	// has already installed them in runSQLiteMigrations.
	const table = "subscription_fair_use_events"
	if !db.Migrator().HasTable(table) {
		if err := ReconcileFairUseTelemetrySchema(db); err != nil {
			return err
		}
		if !db.Migrator().HasTable(table) {
			return fmt.Errorf("observation retention table missing before index migration")
		}
	}
	if !db.Migrator().HasIndex(table, observationRetentionIndex) {
		path := observationRetentionSchemaVersion
		if IsSQLite(db) {
			path = "sqlite/0056_observation_retention_index.sql"
		}
		ddl, err := migrations.Files.ReadFile(path)
		if err != nil {
			return err
		}
		if err := db.Exec(string(ddl)).Error; err != nil {
			return fmt.Errorf("create observation retention index: %w", err)
		}
	}
	indexes, err := db.Migrator().GetIndexes(table)
	if err != nil {
		return err
	}
	valid := false
	for _, index := range indexes {
		if index.Name() == observationRetentionIndex {
			valid = slices.Equal(index.Columns(), []string{"occurred_at", "id"})
		}
	}
	if !valid {
		return fmt.Errorf("observation retention index has unexpected columns; inspect before startup")
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: observationRetentionSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
