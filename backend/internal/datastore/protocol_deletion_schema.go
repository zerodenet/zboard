package datastore

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const protocolDeletionSchemaVersion = "0075_protocol_deletion.up.sql"

// A receipt is authority: never repair lost deletion metadata on startup or copy.
func preflightProtocolDeletionSchema(db *gorm.DB) error {
	tables, err := nodeAssetTables(db)
	if err != nil {
		return err
	}
	if !tables["schema_migrations"] {
		return nil
	}
	var n int64
	if err := db.Table("schema_migrations").Where("version = ?", protocolDeletionSchemaVersion).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	return validateProtocolDeletionSchema(db)
}
func validateProtocolDeletionSchema(db *gorm.DB) error {
	columns, _, err := projectionColumns(db, "protocol_endpoints")
	if err != nil {
		return err
	}
	c, ok := columns["deleted_at"]
	kind := "datetime(3)"
	if IsSQLite(db) {
		kind = "datetime"
	}
	if !ok || c.kind != kind || !c.nullable {
		return fmt.Errorf("recorded protocol deletion metadata missing or incompatible; restore before startup or copy")
	}
	indexes, err := projectionTableIndexes(db, "protocol_endpoints")
	if err != nil {
		return err
	}
	for _, idx := range indexes {
		if idx == (projectionIndex{columns: "deleted_at,id", unique: false}) {
			return nil
		}
	}
	return fmt.Errorf("protocol deletion seek index is missing or incompatible")
}
func reconcileProtocolDeletionSchema(db *gorm.DB) error {
	if err := preflightProtocolDeletionSchema(db); err != nil {
		return err
	}
	columns, _, err := projectionColumns(db, "protocol_endpoints")
	if err != nil {
		return err
	}
	asset := protocolDeletionSchemaVersion
	if IsSQLite(db) {
		asset = "sqlite/0075_protocol_deletion.sql"
	}
	data, err := migrations.Files.ReadFile(asset)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(data))
	if err != nil {
		return err
	}
	if _, ok := columns["deleted_at"]; !ok {
		if err := db.Exec(statements[0]).Error; err != nil {
			return err
		}
	}
	if !db.Migrator().HasIndex("protocol_endpoints", "idx_protocol_endpoints_deletion") {
		if err := db.Exec(statements[1]).Error; err != nil {
			return err
		}
	}
	if err := validateProtocolDeletionSchema(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: protocolDeletionSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
