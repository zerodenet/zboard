package datastore

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const nodeAssetSchemaVersion = "0059_node_asset_identity.up.sql"

type nodeAssetSchema struct {
	nodes, receipt, column bool
}

// Inspect before AutoMigrate can replace lost evidence. This is structural,
// not a scan that guesses the identity of old events or empty current rows.
func preflightNodeAssetSchema(db *gorm.DB) error {
	_, err := inspectNodeAssetSchema(db)
	return err
}

func inspectNodeAssetSchema(db *gorm.DB) (nodeAssetSchema, error) {
	var state nodeAssetSchema
	if db == nil {
		return state, errors.New("node asset schema requires database")
	}
	tables, err := nodeAssetTables(db)
	if err != nil {
		return state, fmt.Errorf("inspect node asset tables: %w", err)
	}
	state.nodes = tables["nodes"]
	if tables["schema_migrations"] {
		var count int64
		if err := db.Table("schema_migrations").Where("version = ?", nodeAssetSchemaVersion).Count(&count).Error; err != nil {
			return state, err
		}
		state.receipt = count != 0
	}
	if state.nodes {
		state.column, err = nodeAssetColumn(db)
		if err != nil {
			return state, fmt.Errorf("inspect node asset column: %w", err)
		}
	}
	if state.receipt && (!state.nodes || !state.column) {
		return state, errors.New("recorded node asset identity is missing; restore before startup or copy")
	}
	return state, nil
}

// Consume every row and Close before any follow-up read, DDL or write. No
// metadata error is treated as absence. Selected current database only.
func nodeAssetTables(db *gorm.DB) (tables map[string]bool, err error) {
	query := "SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME IN ('nodes', 'schema_migrations')"
	if IsSQLite(db) {
		query = "SELECT name FROM sqlite_master WHERE type = 'table' AND name IN ('nodes', 'schema_migrations')"
	}
	rows, err := db.Raw(query).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	tables = make(map[string]bool, 2)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables[strings.ToLower(name)] = true
	}
	return tables, rows.Err()
}

func nodeAssetColumn(db *gorm.DB) (present bool, err error) {
	query := "SELECT COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'nodes' AND COLUMN_NAME = 'asset_tag'"
	if IsSQLite(db) {
		query = "PRAGMA table_info(nodes)"
	}
	rows, err := db.Raw(query).Rows()
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var kind string
		var def sql.NullString
		required := false
		if IsSQLite(db) {
			var ordinal, notnull, pk int
			var name string
			if err := rows.Scan(&ordinal, &name, &kind, &notnull, &def, &pk); err != nil {
				return false, err
			}
			if !strings.EqualFold(name, "asset_tag") {
				continue
			}
			required = notnull != 0
		} else {
			var nullable string
			if err := rows.Scan(&kind, &nullable, &def); err != nil {
				return false, err
			}
			required = nullable == "NO"
		}
		expected := "varchar(64)"
		if IsSQLite(db) {
			expected = "text"
		}
		emptyDefault := def.Valid && def.String == ""
		if IsSQLite(db) {
			// PRAGMA reports the SQL literal, not its value. Accept only the
			// two generated empty literals; DEFAULT '''' means one quote.
			emptyDefault = def.Valid && (def.String == "''" || def.String == "\"\"")
		}
		if !strings.EqualFold(strings.TrimSpace(kind), expected) || !required || !emptyDefault {
			return false, errors.New("node asset column shape differs; restore before startup or copy")
		}
		present = true
	}
	return present, rows.Err()
}

// A new receipt authorizes initialization of current assets for FUTURE capture
// only. Preserve already generated labels after interrupted initialization.
// With a recorded receipt, empty individual rows remain explicitly unknown.
func reconcileNodeAssetSchema(db *gorm.DB) error {
	state, err := inspectNodeAssetSchema(db)
	if err != nil {
		return err
	}
	if !state.nodes {
		return errors.New("node asset identity requires nodes table")
	}
	if state.receipt {
		return nil
	}
	if !state.column {
		definition := "varchar(64) NOT NULL DEFAULT ''"
		if IsSQLite(db) {
			definition = "TEXT NOT NULL DEFAULT ''"
		}
		if err := db.Exec("ALTER TABLE nodes ADD COLUMN asset_tag " + definition).Error; err != nil {
			return err
		}
	}
	for {
		var ids []uint
		if err := db.Table("nodes").Where("asset_tag = ''").Order("id").Limit(200).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			tag, err := model.NewNodeAssetTag()
			if err != nil {
				return err
			}
			// Table map intentionally bypasses the create-only model field. Conditional
			// write cannot overwrite a label established by another initializing host.
			if err := db.Table("nodes").Where("id = ? AND asset_tag = ''", id).Update("asset_tag", tag).Error; err != nil {
				return err
			}
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: nodeAssetSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
