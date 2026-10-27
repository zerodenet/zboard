package datastore

import (
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const hourlyProjectionSchemaVersion = "0054_hourly_usage_projection.up.sql"

var hourlyProjectionColumns = []string{"hourly_pending", "user_id", "subscription_id", "node_id", "record_at"}

func preflightHourlyProjectionSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var applied int64
	if err := db.Table("schema_migrations").Where("version = ?", hourlyProjectionSchemaVersion).Count(&applied).Error; err != nil {
		return err
	}
	if !db.Migrator().HasTable(usageProjectionTable) {
		if applied == 0 {
			return nil
		}
		return fmt.Errorf("hourly projection recovery queue missing; restore before startup")
	}
	present, err := usageProjectionColumns(db)
	if err != nil {
		return err
	}
	for _, column := range hourlyProjectionColumns {
		if !present[column] {
			if applied > 0 {
				return fmt.Errorf("hourly projection recovery column %s missing; restore before startup", column)
			}
			if present["hourly_pending"] {
				var active struct{ ID uint }
				check := db.Table(usageProjectionTable).Select("traffic_record_id AS id").Where("hourly_pending = ?", true).Limit(1).Scan(&active)
				if check.Error != nil {
					return check.Error
				}
				if check.RowsAffected > 0 {
					return fmt.Errorf("incomplete hourly projection schema contains unprojected facts; restore %s before startup", column)
				}
			}
		}
	}
	if present["hourly_pending"] && present["record_at"] {
		var invalid struct{ ID uint }
		check := db.Table(usageProjectionTable).Select("traffic_record_id AS id").Where("hourly_pending = ? AND record_at IS NULL", true).Limit(1).Scan(&invalid)
		if check.Error != nil {
			return check.Error
		}
		if check.RowsAffected > 0 {
			return fmt.Errorf("hourly projection pending timestamp missing; restore before startup")
		}
	}
	return nil
}

func reconcileHourlyProjectionSchema(db *gorm.DB) error {
	if err := preflightHourlyProjectionSchema(db); err != nil {
		return err
	}
	path := hourlyProjectionSchemaVersion
	if IsSQLite(db) {
		path = "sqlite/0054_hourly_usage_projection.sql"
	}
	payload, err := migrations.Files.ReadFile(path)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(payload))
	if err != nil {
		return err
	}
	apply := func(tx *gorm.DB) error {
		present, err := usageProjectionColumns(tx)
		if err != nil {
			return err
		}
		var additions []string
		for i, column := range hourlyProjectionColumns {
			if !present[column] {
				if !IsSQLite(tx) {
					prefix := "ALTER TABLE " + usageProjectionTable + " "
					if !strings.HasPrefix(statements[i], prefix+"ADD COLUMN "+column+" ") {
						return fmt.Errorf("unexpected hourly projection column DDL for %s", column)
					}
					additions = append(additions, strings.TrimPrefix(statements[i], prefix))
					continue
				}
				if err := tx.Exec(statements[i]).Error; err != nil {
					return err
				}
			}
		}
		if len(additions) > 0 {
			if err := tx.Exec("ALTER TABLE " + usageProjectionTable + " " + strings.Join(additions, ", ")).Error; err != nil {
				return err
			}
		}
		for _, statement := range statements[len(hourlyProjectionColumns):] {
			if strings.HasPrefix(statement, "CREATE INDEX ") {
				parts := strings.Fields(statement)
				if tx.Migrator().HasIndex(usageProjectionTable, parts[2]) {
					continue
				}
			}
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}
		// The derived hourly table can be recovered from immutable facts plus
		// exact pending coverage. Reliable intents themselves are never rebuilt.
		needsRecovery, err := hourlyTableNeedsRecovery(tx)
		if err != nil {
			return err
		}
		if needsRecovery {
			if IsSQLite(tx) {
				if err := RebuildTrafficHourly(tx); err != nil {
					return err
				}
			} else {
				// Reuse the MySQL DDL/backfill recovery protocol; its empty-table
				// detection also handles a crash after CREATE but before backfill.
				if err := reconcileMySQLTrafficHourly(tx); err != nil {
					return err
				}
			}
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: hourlyProjectionSchemaVersion, AppliedAt: time.Now().UTC()}).Error
	}
	if IsSQLite(db) {
		return db.Transaction(apply)
	}
	// MySQL DDL commits independently. Every added field/index is inspected
	// before execution; a restart can complete partial DDL without modifying
	// pending flags or replaying existing hourly buckets.
	return apply(db)
}

// PrepareHourlyProjectionRollback is an explicit maintenance operation, never
// part of startup or an automatic down migration. Stop all writers/workers,
// drain their durable intents, then invoke this before starting a pre-0054
// binary. Re-upgrade removes these restored SQLite triggers again without
// replaying existing buckets or dropping pending evidence.
func PrepareHourlyProjectionRollback(db *gorm.DB) error {
	if err := ValidateDiscountRedemptionBinaryRollback(db); err != nil {
		return err
	}
	if err := requireNoLiveTrafficMetaArchiveReferences(db); err != nil {
		return err
	}
	if err := preflightHourlyProjectionSchema(db); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var version struct{ Version string }
		marked := tx.Table("schema_migrations").Select("version").Where("version = ?", hourlyProjectionSchemaVersion).Limit(1).Scan(&version)
		if marked.Error != nil {
			return marked.Error
		}
		if marked.RowsAffected == 0 {
			return fmt.Errorf("hourly projection rollback requires completed migration 0054")
		}
		var pending int64
		if err := tx.Table(usageProjectionTable).Count(&pending).Error; err != nil {
			return err
		}
		if pending != 0 {
			return fmt.Errorf("drain all metering usage projection intents before hourly rollback")
		}
		needsRecovery, err := hourlyTableNeedsRecovery(tx)
		if err != nil {
			return err
		}
		if needsRecovery {
			return fmt.Errorf("recover hourly projection with the current version before hourly rollback")
		}
		if IsSQLite(tx) {
			for _, name := range []string{"traffic_hourly_replace", "traffic_hourly_insert", "traffic_hourly_update", "traffic_hourly_delete"} {
				if err := tx.Exec("DROP TRIGGER IF EXISTS " + name).Error; err != nil {
					return err
				}
			}
			for _, statement := range sqliteTrafficHourlyTriggers() {
				if err := tx.Exec(statement).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}
