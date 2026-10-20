package datastore

import (
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const entitlementFundingSchemaVersion = "0049_entitlement_funding.up.sql"

func preflightEntitlementFundingSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var applied int64
	if err := db.Table("schema_migrations").Where("version = ?", entitlementFundingSchemaVersion).Count(&applied).Error; err != nil {
		return err
	}
	if applied == 0 {
		return nil
	}
	for _, field := range []struct{ table, column string }{{"order_fundings", "entitlement_minor"}, {"orders", "change_snapshot"}} {
		if !db.Migrator().HasTable(field.table) || !db.Migrator().HasColumn(field.table, field.column) {
			return fmt.Errorf("entitlement funding source %s.%s missing; restore before startup", field.table, field.column)
		}
	}
	return nil
}
func reconcileEntitlementFundingSchema(db *gorm.DB) error {
	if err := preflightEntitlementFundingSchema(db); err != nil {
		return err
	}
	path := entitlementFundingSchemaVersion
	if IsSQLite(db) {
		path = "sqlite/0049_entitlement_funding.sql"
	}
	payload, err := migrations.Files.ReadFile(path)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(payload))
	if err != nil {
		return err
	}
	if !db.Migrator().HasColumn("order_fundings", "entitlement_minor") {
		if err := db.Exec(statements[0]).Error; err != nil {
			return err
		}
	}
	if !IsSQLite(db) && !db.Migrator().HasConstraint("order_fundings", "ck_order_funding_entitlement") {
		if err := db.Exec(statements[1]).Error; err != nil {
			return err
		}
	}
	if !IsSQLite(db) {
		columns, err := db.Migrator().ColumnTypes("orders")
		if err != nil {
			return err
		}
		wide := false
		for _, column := range columns {
			if column.Name() == "change_snapshot" {
				wide = strings.EqualFold(column.DatabaseTypeName(), "longtext")
			}
		}
		if !wide {
			if err := db.Exec(statements[2]).Error; err != nil {
				return fmt.Errorf("expand full replacement source snapshot: %w", err)
			}
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: entitlementFundingSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
