package datastore

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

const entitlementEpochSchemaVersion = "0048_entitlement_epochs.up.sql"

var epochSourceColumns = map[string][]string{
	"subscriptions":        {"entitlement_epoch"},
	"protocol_credentials": {"entitlement_epoch", "current_scope"},
	"traffic_records":      {"entitlement_epoch"},
	"quota_events":         {"entitlement_epoch"},
	"closed_entitlements":  {"subscription_id", "epoch", "user_id", "transition_order_id", "flow_total", "closing_flow_used", "flow_used", "traffic_calc_mode", "start_at", "end_at", "closed_at", "updated_at"},
}

func preflightEntitlementEpochSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	var recorded int64
	if db.Migrator().HasTable("schema_migrations") {
		if err := db.Table("schema_migrations").Where("version = ?", entitlementEpochSchemaVersion).Count(&recorded).Error; err != nil {
			return err
		}
	}
	if recorded == 0 {
		if !db.Migrator().HasTable("closed_entitlements") {
			return nil
		}
		var count int64
		if err := db.Table("closed_entitlements").Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return nil
		}
	}
	for table, columns := range epochSourceColumns {
		if !db.Migrator().HasTable(table) {
			return fmt.Errorf("entitlement epoch source table %s missing; restore before startup", table)
		}
		metadata, err := db.Migrator().ColumnTypes(table)
		if err != nil {
			return err
		}
		present := map[string]bool{}
		for _, col := range metadata {
			present[col.Name()] = true
		}
		for _, col := range columns {
			if !present[col] {
				return fmt.Errorf("entitlement epoch source %s.%s missing; restore before startup", table, col)
			}
		}
	}
	var missing []uint
	if err := db.Table("subscriptions AS current").Select("current.id").Where("current.entitlement_epoch > 0 AND (SELECT COUNT(*) FROM closed_entitlements old WHERE old.subscription_id = current.id AND old.epoch < current.entitlement_epoch) <> current.entitlement_epoch").Limit(1).Pluck("current.id", &missing).Error; err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("closed entitlement source history missing for subscription%d; restore before startup", missing[0])
	}
	var invalid []uint
	if err := db.Table("protocol_credentials AS credential").Select("credential.id").Joins("JOIN subscriptions AS current ON current.id = credential.subscription_id").Where("credential.current_scope IS NOT NULL AND (credential.current_scope <> 1 OR credential.entitlement_epoch <> current.entitlement_epoch)").Limit(1).Pluck("credential.id", &invalid).Error; err != nil {
		return err
	}
	if len(invalid) > 0 {
		return fmt.Errorf("credential%d current entitlement scope is inconsistent; restore before startup", invalid[0])
	}
	return nil
}
func reconcileEntitlementEpochSchema(db *gorm.DB) error {
	if err := preflightEntitlementEpochSchema(db); err != nil {
		return err
	}
	for _, field := range []struct{ table, column string }{
		{"subscriptions", "entitlement_epoch"}, {"protocol_credentials", "entitlement_epoch"}, {"protocol_credentials", "current_scope"}, {"traffic_records", "entitlement_epoch"}, {"quota_events", "entitlement_epoch"},
	} {
		if db.Migrator().HasColumn(field.table, field.column) {
			continue
		}
		definition := "bigint unsigned NOT NULL DEFAULT 0"
		if IsSQLite(db) {
			definition = "INTEGER NOT NULL DEFAULT 0"
		}
		if field.column == "current_scope" {
			definition = "tinyint unsigned NULL DEFAULT 1"
			if IsSQLite(db) {
				definition = "INTEGER NULL DEFAULT 1"
			}
		}
		if err := db.Exec("ALTER TABLE " + field.table + " ADD COLUMN " + field.column + " " + definition).Error; err != nil {
			return err
		}
	}
	path := entitlementEpochSchemaVersion
	if IsSQLite(db) {
		path = "sqlite/0048_entitlement_epochs.sql"
	}
	payload, err := migrations.Files.ReadFile(path)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(payload))
	if err != nil {
		return err
	}
	if !db.Migrator().HasTable("closed_entitlements") {
		if err := db.Exec(statements[5]).Error; err != nil {
			return err
		}
	}
	indexes, err := db.Migrator().GetIndexes("protocol_credentials")
	if err != nil {
		return err
	}
	correct := false
	for _, idx := range indexes {
		if idx.Name() == "ux_credential_current_endpoint" {
			unique, _ := idx.Unique()
			if !unique || strings.Join(idx.Columns(), ",") != "subscription_id,protocol_endpoint_id,current_scope" {
				return fmt.Errorf("credential current ownership index has an invalid shape; repair before startup")
			}
			correct = true
		}
	}
	if !correct {
		if err := db.Exec(statements[6]).Error; err != nil {
			return err
		}
	}
	// Remove only the exact former scope constraint. Principal, opaque identity
	// and unrelated uniqueness remain enforced. Never choose/drop duplicate rows.
	for _, idx := range indexes {
		unique, _ := idx.Unique()
		if unique && strings.Join(idx.Columns(), ",") == "subscription_id,protocol_endpoint_id" {
			if err := db.Migrator().DropIndex("protocol_credentials", idx.Name()); err != nil {
				return err
			}
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: entitlementEpochSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
