package datastore

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const automaticRecurringApprovalSchemaVersion = "0057_automatic_recurring_approval.up.sql"

// Run before SQLite AutoMigrate: once authority could be stored, missing
// authority columns must not be replaced with defaults by schema repair.
func preflightAutomaticRecurringApprovalSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	var applied int64
	if db.Migrator().HasTable("schema_migrations") {
		if err := db.Table("schema_migrations").Where("version = ?", automaticRecurringApprovalSchemaVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	const table = "subscription_automatic_renewals"
	version := db.Migrator().HasColumn(table, "agreement_version")
	snapshot := db.Migrator().HasColumn(table, "approval_snapshot")
	if applied > 0 && (!version || !snapshot) {
		return fmt.Errorf("automatic recurring approval authority missing; restore before startup")
	}
	if !version || !snapshot {
		var evidence struct{ Present int }
		if snapshot {
			if err := db.Table(table).Where("approval_snapshot <> ?", "").Select("1 AS present").Limit(1).Scan(&evidence).Error; err != nil {
				return err
			}
		}
		if version {
			if err := db.Table(table).Where("agreement_version <> ?", 1).Select("1 AS present").Limit(1).Scan(&evidence).Error; err != nil {
				return err
			}
		}
		if evidence.Present > 0 {
			return fmt.Errorf("incomplete automatic recurring approval contains authority; restore before startup")
		}
		return nil
	}
	// Complete structure requires no per-item startup audit. A damaged approval
	// is isolated by the worker without hiding other accounts or revocation.
	return nil
}

func reconcileAutomaticRecurringApprovalSchema(db *gorm.DB) error {
	if err := preflightAutomaticRecurringApprovalSchema(db); err != nil {
		return err
	}
	if !db.Migrator().HasTable("subscription_automatic_renewals") {
		return fmt.Errorf("automatic recurring approval table missing; restore before startup")
	}
	path := automaticRecurringApprovalSchemaVersion
	if IsSQLite(db) {
		path = "sqlite/0057_automatic_recurring_approval.sql"
	}
	raw, err := migrations.Files.ReadFile(path)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(raw))
	if err != nil {
		return err
	}
	for i, column := range []string{"agreement_version", "approval_snapshot"} {
		if !db.Migrator().HasColumn("subscription_automatic_renewals", column) {
			if err := db.Exec(statements[i]).Error; err != nil {
				return fmt.Errorf("add automatic recurring approval %s: %w", column, err)
			}
		}
	}
	if err := preflightAutomaticRecurringApprovalSchema(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: automaticRecurringApprovalSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
