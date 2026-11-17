package datastore

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const purchaseSnapshotVersion = "0037_commercial_purchase_snapshot.up.sql"

// The single additive DDL can commit before MySQL's migration receipt. Retry
// both interrupted migrations and already-recorded schemas without replacing
// existing snapshots or inferring historical policies from the current catalog.
func reconcilePurchaseSnapshotSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.Order{}) {
		return fmt.Errorf("purchase snapshot requires the existing orders table")
	}
	if !db.Migrator().HasColumn(&model.Order{}, "PurchaseSnapshot") {
		path := purchaseSnapshotVersion
		if IsSQLite(db) {
			path = "sqlite/0037_commercial_purchase_snapshot.sql"
		}
		payload, err := migrations.Files.ReadFile(path)
		if err != nil {
			return err
		}
		if err := db.Exec(string(payload)).Error; err != nil {
			return fmt.Errorf("add purchase snapshot: %w", err)
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: purchaseSnapshotVersion, AppliedAt: time.Now().UTC()}).Error
}
