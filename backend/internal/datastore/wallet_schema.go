package datastore

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const walletSchemaVersion = "0038_wallet_topups.up.sql"

// A missing money table is loss of authoritative facts, not an empty wallet.
// Check before SQLite AutoMigrate could silently recreate a missing table.
func preflightWalletSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	accounts, entries := db.Migrator().HasTable("wallet_accounts"), db.Migrator().HasTable("wallet_entries")
	if accounts && entries {
		return nil
	}
	var applied int64
	if db.Migrator().HasTable("schema_migrations") {
		if err := db.Table("schema_migrations").Where("version = ?", walletSchemaVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	if applied > 0 {
		return fmt.Errorf("wallet financial tables are missing after migration; restore and reconcile financial evidence before startup")
	}
	for _, table := range []struct {
		name   string
		exists bool
	}{{"wallet_accounts", accounts}, {"wallet_entries", entries}} {
		if table.exists {
			var present struct{ Found int }
			if err := db.Table(table.name).Select("1 AS found").Limit(1).Scan(&present).Error; err != nil {
				return err
			}
			if present.Found == 1 {
				return fmt.Errorf("incomplete wallet schema contains financial evidence; restore its missing counterpart before startup")
			}
		}
	}
	return nil
}
func reconcileWalletSchema(db *gorm.DB) error {
	if err := preflightWalletSchema(db); err != nil {
		return err
	}
	path := walletSchemaVersion
	if IsSQLite(db) {
		path = "sqlite/0038_wallet_topups.sql"
	}
	payload, err := migrations.Files.ReadFile(path)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(payload))
	if err != nil {
		return err
	}
	if !IsSQLite(db) {
		var nullable string
		if err := db.Raw("SELECT IS_NULLABLE FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'orders' AND column_name = 'plan_sku_id'").Scan(&nullable).Error; err != nil {
			return err
		}
		if nullable == "" {
			return fmt.Errorf("wallet requires the existing order SKU reference")
		}
		if nullable != "YES" {
			if err := db.Exec(statements[0]).Error; err != nil {
				return err
			}
		}
		statements = statements[1:]
	}
	for i, table := range []string{"wallet_accounts", "wallet_entries"} {
		if !db.Migrator().HasTable(table) {
			if err := db.Exec(statements[i]).Error; err != nil {
				return err
			}
		}
	}
	// SQLite's model inventory normally owns indexes; retain explicit recovery
	// for a schema whose additive DDL completed before its migration receipt.
	if IsSQLite(db) {
		for _, statement := range statements[2:] {
			if err := db.Exec(statement).Error; err != nil {
				return err
			}
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: walletSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
