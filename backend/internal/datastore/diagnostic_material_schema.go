package datastore

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const diagnosticMaterialVersion = "0062_traffic_diagnostic_material_retention.up.sql"
const diagnosticMaterialTable = "traffic_diagnostic_materials"

var diagnosticMaterialIndexes = []retentionMaterialIndex{
	{diagnosticMaterialTable, "idx_traffic_diagnostic_review", []string{"retired_at", "review_at", "traffic_record_id"}},
}

// Classification and original body receipts are durable evidence. Missing
// recorded fields/tables are never repaired as empty/unretired material.
func preflightDiagnosticMaterialSchema(db *gorm.DB) error {
	if db == nil {
		return errors.New("database is required")
	}
	tables, err := migrationTableSet(db)
	if err != nil {
		return err
	}
	var receipts int64
	if tables["schema_migrations"] {
		if err := db.Table("schema_migrations").Where("version = ?", diagnosticMaterialVersion).Count(&receipts).Error; err != nil {
			return err
		}
	}
	if !tables[diagnosticMaterialTable] {
		if receipts > 0 {
			return errors.New("recorded diagnostic provenance table missing; restore before startup/copy")
		}
		return nil
	}
	columns, primary, err := projectionColumns(db, diagnosticMaterialTable)
	if err != nil {
		return err
	}
	if strings.Join(primary, ",") != "traffic_record_id" || len(columns) != 6 {
		return errors.New("diagnostic provenance columns/primary key differ; restore before startup/copy")
	}
	for _, name := range []string{"traffic_record_id", "source_bytes", "source_sha256", "retired_at", "review_at", "created_at"} {
		column, ok := columns[name]
		if !ok {
			return fmt.Errorf("diagnostic provenance column %s missing; restore before startup/copy", name)
		}
		kind := "datetime(3)"
		if IsSQLite(db) {
			kind = "datetime"
		}
		if name == "traffic_record_id" || name == "source_bytes" {
			kind = "bigint unsigned"
			if IsSQLite(db) {
				kind = "integer"
			}
		}
		if name == "source_sha256" {
			kind = "varchar(64)"
			if IsSQLite(db) {
				kind = "text"
			}
		}
		if column.kind != kind || column.nullable != (name == "retired_at") {
			return fmt.Errorf("diagnostic provenance column %s shape differs", name)
		}
		if !IsSQLite(db) && name == "source_sha256" && column.collation != "ascii_bin" {
			return errors.New("diagnostic provenance digest collation differs")
		}
	}
	// A body owner must retain its source fact, including compacted receipts.
	if IsSQLite(db) {
		var keys []struct {
			Table    string `gorm:"column:table"`
			From     string `gorm:"column:from"`
			To       string `gorm:"column:to"`
			OnDelete string `gorm:"column:on_delete"`
			OnUpdate string `gorm:"column:on_update"`
			Seq      int
		}
		if err := db.Raw("PRAGMA foreign_key_list(traffic_diagnostic_materials)").Scan(&keys).Error; err != nil {
			return err
		}
		if len(keys) != 1 || keys[0].Table != "traffic_records" || keys[0].From != "traffic_record_id" || keys[0].To != "id" || keys[0].OnDelete != "RESTRICT" || keys[0].Seq != 0 || keys[0].OnUpdate != "RESTRICT" {
			return errors.New("diagnostic provenance source protection differs")
		}
	} else {
		var keys []struct {
			ColumnName, ReferencedTableName, ReferencedColumnName, DeleteRule, UpdateRule string
			OrdinalPosition                                                               int
		}
		q := `SELECT k.COLUMN_NAME AS column_name,k.REFERENCED_TABLE_NAME AS referenced_table_name,k.REFERENCED_COLUMN_NAME AS referenced_column_name,k.ORDINAL_POSITION AS ordinal_position,r.DELETE_RULE AS delete_rule,r.UPDATE_RULE AS update_rule FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE k JOIN INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS r ON r.CONSTRAINT_SCHEMA=k.CONSTRAINT_SCHEMA AND r.TABLE_NAME=k.TABLE_NAME AND r.CONSTRAINT_NAME=k.CONSTRAINT_NAME WHERE k.TABLE_SCHEMA=DATABASE() AND k.TABLE_NAME=? AND k.REFERENCED_TABLE_NAME IS NOT NULL`
		if err := db.Raw(q, diagnosticMaterialTable).Scan(&keys).Error; err != nil {
			return err
		}
		if len(keys) != 1 || keys[0].ColumnName != "traffic_record_id" || keys[0].ReferencedTableName != "traffic_records" || keys[0].ReferencedColumnName != "id" || keys[0].OrdinalPosition != 1 || keys[0].DeleteRule != "RESTRICT" || keys[0].UpdateRule != "RESTRICT" {
			return errors.New("diagnostic provenance source protection differs")
		}
	}
	return nil
}

func reconcileDiagnosticMaterialSchema(db *gorm.DB) error {
	if err := preflightDiagnosticMaterialSchema(db); err != nil {
		return err
	}
	asset := diagnosticMaterialVersion
	if IsSQLite(db) {
		asset = "sqlite/0062_traffic_diagnostic_material_retention.sql"
	}
	raw, err := migrations.Files.ReadFile(asset)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(raw))
	if err != nil {
		return err
	}
	if len(statements) != 2 {
		return errors.New("unexpected diagnostic material migration inventory")
	}
	if !db.Migrator().HasTable(diagnosticMaterialTable) {
		if err := db.Exec(statements[0]).Error; err != nil {
			return err
		}
	}
	if err := preflightDiagnosticMaterialSchema(db); err != nil {
		return err
	}
	for _, spec := range diagnosticMaterialIndexes {
		if err := ensureRetentionMaterialIndex(db, spec); err != nil {
			return err
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: diagnosticMaterialVersion, AppliedAt: time.Now().UTC()}).Error
}
