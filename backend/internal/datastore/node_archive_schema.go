package datastore

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const nodeArchiveSchemaVersion = "0073_node_archive.up.sql"

// A receipt is authority: never repair lost archive metadata on startup or copy.
func preflightNodeArchiveSchema(db *gorm.DB) error {
	tables, err := nodeAssetTables(db)
	if err != nil {
		return err
	}
	if !tables["schema_migrations"] {
		return nil
	}
	var n int64
	if err := db.Table("schema_migrations").Where("version = ?", nodeArchiveSchemaVersion).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	return validateNodeArchiveSchema(db)
}
func validateNodeArchiveSchema(db *gorm.DB) error {
	columns, _, err := projectionColumns(db, "nodes")
	if err != nil {
		return err
	}
	c, ok := columns["archived_at"]
	kind := "datetime(3)"
	if IsSQLite(db) {
		kind = "datetime"
	}
	if !ok || c.kind != kind || !c.nullable {
		return fmt.Errorf("recorded node archive metadata missing or incompatible; restore before startup or copy")
	}
	indexes, err := projectionTableIndexes(db, "nodes")
	if err != nil {
		return err
	}
	for _, idx := range indexes {
		if idx == (projectionIndex{columns: "archived_at,id", unique: false}) {
			return nil
		}
	}
	return fmt.Errorf("node archive seek index is missing or incompatible")
}
func reconcileNodeArchiveSchema(db *gorm.DB) error {
	if err := preflightNodeArchiveSchema(db); err != nil {
		return err
	}
	columns, _, err := projectionColumns(db, "nodes")
	if err != nil {
		return err
	}
	asset := nodeArchiveSchemaVersion
	if IsSQLite(db) {
		asset = "sqlite/0073_node_archive.sql"
	}
	data, err := migrations.Files.ReadFile(asset)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(data))
	if err != nil {
		return err
	}
	if _, ok := columns["archived_at"]; !ok {
		if err := db.Exec(statements[0]).Error; err != nil {
			return err
		}
	}
	if !db.Migrator().HasIndex("nodes", "idx_nodes_archive") {
		if err := db.Exec(statements[1]).Error; err != nil {
			return err
		}
	}
	if err := validateNodeArchiveSchema(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: nodeArchiveSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
