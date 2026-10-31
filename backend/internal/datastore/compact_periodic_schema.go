package datastore

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

const compactPeriodicVersion = "0076_compact_periodic_history.up.sql"

func validateCompactPeriodicSchema(db *gorm.DB) error {
	cols, _, err := projectionColumns(db, "job_schedules")
	if err != nil {
		return err
	}
	flag, ok := cols["compact_history"]
	if !ok || flag.nullable || (flag.kind != "tinyint(1)" && flag.kind != "numeric") {
		return fmt.Errorf("compact periodic history policy column differs")
	}
	id, ok := cols["compact_run_id"]
	if !ok || id.nullable || (id.kind != "varchar(36)" && id.kind != "text") {
		return fmt.Errorf("compact periodic history pointer differs")
	}
	return nil
}
func reconcileCompactPeriodicSchema(db *gorm.DB) error {
	var n int64
	if err := db.Table("schema_migrations").Where("version = ?", compactPeriodicVersion).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return validateCompactPeriodicSchema(db)
	}
	asset := compactPeriodicVersion
	if IsSQLite(db) {
		asset = "sqlite/0076_compact_periodic_history.sql"
	}
	raw, err := migrations.Files.ReadFile(asset)
	if err != nil {
		return err
	}
	stmts, err := splitMigrationStatements(string(raw))
	if err != nil {
		return err
	}
	cols, _, err := projectionColumns(db, "job_schedules")
	if err != nil {
		return err
	}
	for i, col := range []string{"compact_history", "compact_run_id"} {
		if _, ok := cols[col]; !ok {
			if err := db.Exec(stmts[i]).Error; err != nil {
				return err
			}
		}
	}
	if err := validateCompactPeriodicSchema(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: compactPeriodicVersion, AppliedAt: time.Now().UTC()}).Error
}

func preflightCompactPeriodicSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var n int64
	if err := db.Table("schema_migrations").Where("version = ?", compactPeriodicVersion).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	return validateCompactPeriodicSchema(db)
}
