package datastore

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

const entitlementValueSchemaVersion = "0042_entitlement_value_snapshot.up.sql"

func preflightEntitlementValueSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var applied int64
	if err := db.Table("schema_migrations").Where("version = ?", entitlementValueSchemaVersion).Count(&applied).Error; err != nil {
		return err
	}
	if applied > 0 && (!db.Migrator().HasTable("orders") || !db.Migrator().HasColumn("orders", "entitlement_value_snapshot")) {
		return fmt.Errorf("captured entitlement value evidence missing after migration; restore before startup")
	}
	return nil
}
func reconcileEntitlementValueSchema(db *gorm.DB) error {
	if err := preflightEntitlementValueSchema(db); err != nil {
		return err
	}
	if !db.Migrator().HasTable(&model.Order{}) {
		return fmt.Errorf("entitlement value evidence requires orders")
	}
	if !db.Migrator().HasColumn(&model.Order{}, "EntitlementValueSnapshot") {
		path := entitlementValueSchemaVersion
		if IsSQLite(db) {
			path = "sqlite/0042_entitlement_value_snapshot.sql"
		}
		payload, err := migrations.Files.ReadFile(path)
		if err != nil {
			return err
		}
		if err := db.Exec(string(payload)).Error; err != nil {
			return fmt.Errorf("add entitlement value evidence: %w", err)
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: entitlementValueSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
