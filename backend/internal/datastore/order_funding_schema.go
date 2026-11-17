package datastore

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

const orderFundingSchemaVersion = "0040_order_funding.up.sql"

func preflightOrderFundingSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	var applied int64
	if db.Migrator().HasTable("schema_migrations") {
		if err := db.Table("schema_migrations").Where("version = ?", orderFundingSchemaVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	if !db.Migrator().HasTable("order_fundings") {
		if applied > 0 {
			return fmt.Errorf("order funding financial table is missing; restore reservations and reconcile before startup")
		}
		return nil
	}
	for _, column := range []string{"order_id", "user_id", "currency", "use_balance", "amount_minor", "wallet_minor", "cash_minor", "state", "expires_at", "captured_at", "released_at", "release_reason", "created_at", "updated_at"} {
		if !db.Migrator().HasColumn("order_fundings", column) {
			return fmt.Errorf("order funding financial column %s missing; restore before startup", column)
		}
	}
	return nil
}
func reconcileOrderFundingSchema(db *gorm.DB) error {
	if err := preflightOrderFundingSchema(db); err != nil {
		return err
	}
	path := orderFundingSchemaVersion
	if IsSQLite(db) {
		path = "sqlite/0040_order_funding.sql"
	}
	payload, err := migrations.Files.ReadFile(path)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(payload))
	if err != nil {
		return err
	}
	if !db.Migrator().HasTable("order_fundings") {
		if err := createHistoricalTable(db, "order_fundings", statements[0]); err != nil {
			return err
		}
	}
	if IsSQLite(db) {
		for _, statement := range statements[1:] {
			if err := db.Exec(statement).Error; err != nil {
				return err
			}
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: orderFundingSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
