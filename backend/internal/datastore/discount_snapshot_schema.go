package datastore

import (
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const discountSnapshotSchemaVersion = "0055_discount_purchase_snapshot.up.sql"

// Once discount evidence could have been committed, missing purchase facts must
// be restored rather than replaced by an empty snapshot during AutoMigrate.
func preflightDiscountSnapshotSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var applied int64
	if err := db.Table("schema_migrations").Where("version = ?", discountSnapshotSchemaVersion).Count(&applied).Error; err != nil {
		return err
	}
	if applied > 0 && (!db.Migrator().HasTable("orders") || !db.Migrator().HasColumn("orders", "purchase_snapshot")) {
		return fmt.Errorf("purchase snapshot evidence missing; restore before startup")
	}
	return nil
}

func reconcileDiscountSnapshotSchema(db *gorm.DB) error {
	if err := preflightDiscountSnapshotSchema(db); err != nil {
		return err
	}
	if !db.Migrator().HasColumn("orders", "purchase_snapshot") {
		// A legacy database can have recorded 0037 while its column is missing.
		// Recover through the existing compatibility path before applying 0055.
		// The preflight above still forbids inventing lost post-0055 evidence.
		if err := reconcilePurchaseSnapshotSchema(db); err != nil {
			return err
		}
	}
	if !IsSQLite(db) {
		var dataType string
		if err := db.Raw("SELECT DATA_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'orders' AND COLUMN_NAME = 'purchase_snapshot'").Scan(&dataType).Error; err != nil {
			return err
		}
		switch strings.ToLower(dataType) {
		case "text", "mediumtext", "longtext":
			// Never shrink an already wider installation on restart.
		case "varchar", "tinytext":
			payload, err := migrations.Files.ReadFile(discountSnapshotSchemaVersion)
			if err != nil {
				return err
			}
			if err := db.Exec(string(payload)).Error; err != nil {
				return fmt.Errorf("expand purchase snapshot evidence: %w", err)
			}
		default:
			return fmt.Errorf("unexpected purchase snapshot column type %q; restore before startup", dataType)
		}
	}
	// MySQL DDL may commit before this receipt. Re-entry inspects the actual
	// column and never rewrites existing facts, including unknown legacy bytes.
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: discountSnapshotSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
