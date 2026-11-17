package datastore

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"slices"
	"time"
)

const principalMinuteVersion = "0077_principal_minute_buckets.up.sql"

func validatePrincipalMinuteSchema(db *gorm.DB) error {
	cols, pk, err := projectionColumns(db, "principal_statistics_minutes")
	if err != nil {
		return err
	}
	if len(cols) != 5 || !slices.Equal(pk, []string{"scope_type", "scope_id", "minute_at"}) {
		return fmt.Errorf("principal minute bucket shape differs")
	}
	for _, name := range []string{"scope_type", "scope_id", "minute_at", "peak", "sample_count"} {
		kind := "bigint unsigned"
		if IsSQLite(db) {
			kind = "integer"
		}
		if name == "scope_type" {
			kind = "varchar(24)"
			if IsSQLite(db) {
				kind = "text"
			}
		}
		if name == "minute_at" {
			kind = "datetime(3)"
			if IsSQLite(db) {
				kind = "datetime"
			}
		}
		col, ok := cols[name]
		if !ok || col.nullable || col.kind != kind {
			return fmt.Errorf("principal minute column differs: %s", name)
		}
	}
	found, err := principalStatisticsIndex(db, principalStatisticsIndexSpec{"principal_statistics_minutes", "idx_principal_minute_retention", []string{"minute_at", "scope_type", "scope_id"}, false})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("principal minute retention index missing")
	}
	return nil
}
func reconcilePrincipalMinuteSchema(db *gorm.DB) error {
	var n int64
	if err := db.Table("schema_migrations").Where("version = ?", principalMinuteVersion).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return validatePrincipalMinuteSchema(db)
	}
	if !db.Migrator().HasTable("principal_statistics_minutes") {
		asset := principalMinuteVersion
		if IsSQLite(db) {
			asset = "sqlite/0077_principal_minute_buckets.sql"
		}
		raw, err := migrations.Files.ReadFile(asset)
		if err != nil {
			return err
		}
		stmts, err := splitMigrationStatements(string(raw))
		if err != nil {
			return err
		}
		for _, stmt := range stmts {
			if err := db.Exec(stmt).Error; err != nil {
				return err
			}
		}
	} else if !db.Migrator().HasIndex("principal_statistics_minutes", "idx_principal_minute_retention") {
		if err := db.Exec("CREATE INDEX idx_principal_minute_retention ON principal_statistics_minutes(minute_at,scope_type,scope_id)").Error; err != nil {
			return err
		}
	}
	if err := validatePrincipalMinuteSchema(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: principalMinuteVersion, AppliedAt: time.Now().UTC()}).Error
}

func preflightPrincipalMinuteSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var n int64
	if err := db.Table("schema_migrations").Where("version = ?", principalMinuteVersion).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	return validatePrincipalMinuteSchema(db)
}
