package datastore

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const flowCursorCheckpointVersion = "0083_flow_cursor_checkpoints.up.sql"

func flowIdentityCollation(db *gorm.DB, table string) (string, error) {
	var collation string
	if IsSQLite(db) {
		var values []string
		if err := db.Raw("SELECT x.coll FROM pragma_index_list(?) i JOIN pragma_index_xinfo(i.name) x WHERE i.[unique] = 1 AND x.name = 'flow_id' AND x.key = 1", table).Scan(&values).Error; err != nil {
			return "", err
		}
		if len(values) != 1 {
			return "", errors.New("flow identity unique index unavailable")
		}
		collation = values[0]
	} else {
		if err := db.Raw("SELECT COLLATION_NAME FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME='flow_id' AND COLUMN_TYPE='varchar(128)'", table).Scan(&collation).Error; err != nil {
			return "", err
		}
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(collation) {
		return "", errors.New("flow identity collation unavailable")
	}
	return collation, nil
}

func validateFlowCursorCheckpointSchema(db *gorm.DB) error {
	cols, pk, err := projectionColumns(db, "flow_cursor_checkpoints")
	if err != nil {
		return err
	}
	if len(cols) != 9 || !slices.Equal(pk, []string{"node_id", "flow_id"}) {
		return errors.New("flow checkpoint accounting authority shape differs")
	}
	for _, name := range []string{"node_id", "flow_id", "protocol_credential_id", "revision", "raw_bytes", "upload_bytes", "download_bytes", "used_bytes", "is_completed"} {
		kind := "bigint"
		if name == "node_id" || name == "protocol_credential_id" || name == "revision" {
			kind = "bigint unsigned"
		}
		if name == "flow_id" {
			kind = "varchar(128)"
		}
		if name == "is_completed" {
			kind = "tinyint(1)"
		}
		if IsSQLite(db) {
			kind = "integer"
			if name == "flow_id" {
				kind = "text"
			}
			if name == "is_completed" {
				kind = "numeric"
			}
		}
		col, ok := cols[name]
		if !ok || col.nullable || col.kind != kind {
			return fmt.Errorf("flow checkpoint accounting column differs: %s", name)
		}
	}
	want, err := flowIdentityCollation(db, "flow_usages")
	if err != nil {
		return err
	}
	actual, err := flowIdentityCollation(db, "flow_cursor_checkpoints")
	if err != nil {
		return err
	}
	if !strings.EqualFold(want, actual) {
		return errors.New("flow checkpoint identity collation differs from hot authority")
	}
	// The hot row is deliberately removed after transfer. A source FK (including
	// a cascade added outside owned preparation) would destroy the only floor.
	if IsSQLite(db) {
		var refs []map[string]any
		if err := db.Raw("PRAGMA foreign_key_list(flow_cursor_checkpoints)").Scan(&refs).Error; err != nil {
			return err
		}
		if len(refs) != 0 {
			return errors.New("flow checkpoint authority must not have source foreign keys")
		}
	} else {
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='flow_cursor_checkpoints' AND REFERENCED_TABLE_NAME IS NOT NULL").Scan(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return errors.New("flow checkpoint authority must not have source foreign keys")
		}
	}
	cols, pk, err = projectionColumns(db, "flow_cursor_compaction_states")
	if err != nil {
		return err
	}
	if len(cols) != 3 || !slices.Equal(pk, []string{"id"}) {
		return errors.New("flow cursor compaction checkpoint shape differs")
	}
	for _, name := range []string{"id", "after_id", "next_pass_at"} {
		kind := "bigint unsigned"
		if name == "next_pass_at" {
			kind = "datetime(3)"
		}
		if IsSQLite(db) {
			kind = "integer"
			if name == "next_pass_at" {
				kind = "datetime"
			}
		}
		if c, ok := cols[name]; !ok || c.nullable || c.kind != kind {
			return fmt.Errorf("flow compaction column differs: %s", name)
		}
	}
	return nil
}

// Never recreate a recorded missing authority table as empty on startup/copy.
func preflightFlowCursorCheckpointSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var n int64
	if err := db.Table("schema_migrations").Where("version = ?", flowCursorCheckpointVersion).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	return validateFlowCursorCheckpointSchema(db)
}

func reconcileFlowCursorCheckpointSchema(db *gorm.DB) error {
	if err := preflightFlowCursorCheckpointSchema(db); err != nil {
		return err
	}
	asset := flowCursorCheckpointVersion
	if IsSQLite(db) {
		asset = "sqlite/0083_flow_cursor_checkpoints.sql"
	}
	raw, err := migrations.Files.ReadFile(asset)
	if err != nil {
		return err
	}
	collation, err := flowIdentityCollation(db, "flow_usages")
	if err != nil {
		return err
	}
	source := strings.Replace(string(raw), "COLLATE utf8mb4_0900_ai_ci", "COLLATE "+collation, 1)
	if IsSQLite(db) {
		source = strings.Replace(source, "COLLATE BINARY", "COLLATE "+collation, 1)
	}
	stmts, err := splitMigrationStatements(source)
	if err != nil {
		return err
	}
	if len(stmts) != 2 {
		return errors.New("unexpected completed flow migration inventory")
	}
	for i, name := range []string{"flow_cursor_checkpoints", "flow_cursor_compaction_states"} {
		if !db.Migrator().HasTable(name) {
			if err := db.Exec(stmts[i]).Error; err != nil {
				return err
			}
		}
	}
	if err := validateFlowCursorCheckpointSchema(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: flowCursorCheckpointVersion, AppliedAt: time.Now().UTC()}).Error
}
