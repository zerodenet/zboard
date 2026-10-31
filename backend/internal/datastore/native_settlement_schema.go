package datastore

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

const nativeSettlementVersion = "0078_native_minute_settlement.up.sql"

var nativeSettlementColumns = []struct{ table, name, kind string }{
	{"traffic_records", "is_aggregate", "tinyint(1)"}, {"traffic_records", "record_count", "bigint"},
	{"metering_usage_projection_pending", "delta_raw_bytes", "bigint"}, {"metering_usage_projection_pending", "delta_upload_bytes", "bigint"}, {"metering_usage_projection_pending", "delta_download_bytes", "bigint"}, {"metering_usage_projection_pending", "delta_used_bytes", "bigint"}, {"metering_usage_projection_pending", "delta_record_count", "bigint"},
}

func validateNativeSettlementSchema(db *gorm.DB) error {
	for _, spec := range nativeSettlementColumns {
		cols, _, err := projectionColumns(db, spec.table)
		if err != nil {
			return err
		}
		col, ok := cols[spec.name]
		kind := spec.kind
		if IsSQLite(db) {
			kind = "integer"
			if spec.kind == "tinyint(1)" {
				kind = "numeric"
			}
		}
		wantDefault := "0"
		if spec.name == "record_count" {
			wantDefault = "1"
		}
		def := strings.Trim(strings.ToLower(col.defaultSQL), "()'\"")
		if def == "false" {
			def = "0"
		}
		if !ok || col.nullable || col.kind != kind || def != wantDefault {
			return fmt.Errorf("native settlement column differs: %s.%s", spec.table, spec.name)
		}
	}
	return nil
}
func preflightNativeSettlementSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var n int64
	if err := db.Table("schema_migrations").Where("version = ?", nativeSettlementVersion).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	return validateNativeSettlementSchema(db)
}
func reconcileNativeSettlementSchema(db *gorm.DB) error {
	var n int64
	if err := db.Table("schema_migrations").Where("version = ?", nativeSettlementVersion).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return validateNativeSettlementSchema(db)
	}
	asset := nativeSettlementVersion
	if IsSQLite(db) {
		asset = "sqlite/0078_native_minute_settlement.sql"
	}
	raw, err := migrations.Files.ReadFile(asset)
	if err != nil {
		return err
	}
	stmts, err := splitMigrationStatements(string(raw))
	if err != nil {
		return err
	}
	for i, spec := range nativeSettlementColumns {
		cols, _, err := projectionColumns(db, spec.table)
		if err != nil {
			return err
		}
		if _, ok := cols[spec.name]; !ok {
			if err := nativeSettlementDDL(db, stmts[i]); err != nil {
				return err
			}
		}
	}
	if err := validateNativeSettlementSchema(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: nativeSettlementVersion, AppliedAt: time.Now().UTC()}).Error
}

func nativeSettlementDDL(db *gorm.DB, statement string) error {
	if IsSQLite(db) {
		return db.Exec(statement).Error
	}
	return db.Connection(func(conn *gorm.DB) error {
		var previous int
		if err := conn.Raw("SELECT @@SESSION.lock_wait_timeout").Scan(&previous).Error; err != nil {
			return err
		}
		if err := conn.Exec("SET SESSION lock_wait_timeout = 5").Error; err != nil {
			return err
		}
		defer conn.Exec(fmt.Sprintf("SET SESSION lock_wait_timeout = %d", previous))
		return conn.Exec(statement).Error
	})
}
