package datastore

import (
	"errors"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

const principalLookupSchemaVersion = "0046_principal_lookup_index.up.sql"

// All lookup state is derived from canonical credentials/retired digests. A
// missing auxiliary column is rebuildable; it must never become proof that
// every credential has been indexed. Financial/retired-identity preflights
// still run before schema creation and prohibit inventing lost source facts.
func reconcilePrincipalLookupSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable("protocol_credentials") {
		return fmt.Errorf("principal lookup requires canonical credentials")
	}
	columns := []struct{ name, mysql, sqlite string }{
		{"principal_digest", "varchar(64) NOT NULL DEFAULT ''", "TEXT NOT NULL DEFAULT ''"},
		{"secret_principal_digest", "varchar(64) NOT NULL DEFAULT ''", "TEXT NOT NULL DEFAULT ''"},
		{"principal_lookup_version", "tinyint unsigned NOT NULL DEFAULT 0", "INTEGER NOT NULL DEFAULT 0"},
	}
	present, err := principalLookupColumnNames(db)
	if err != nil {
		return fmt.Errorf("inspect protocol_credentials columns: %w", err)
	}
	missing := false
	for _, column := range columns {
		missing = missing || !present[column.name]
	}
	// A DDL commit must never precede invalidation of existing coverage. If a
	// digest is lost, an interruption after ADD would otherwise leave version1
	// rows with the new empty default and make the next startup skip recovery.
	// When version itself is missing, its new DEFAULT0 already invalidates all
	// rows. Empty digests can be legitimate and are not evidence of damage.
	if missing && present["principal_lookup_version"] {
		if err := db.Exec("UPDATE protocol_credentials SET principal_lookup_version = 0 WHERE principal_lookup_version <> 0").Error; err != nil {
			return err
		}
	}
	if IsSQLite(db) {
		// Keep SQLite's existing sequential additive DDL and definitions.
		for _, column := range columns {
			if present[column.name] {
				continue
			}
			if err := db.Exec("ALTER TABLE protocol_credentials ADD COLUMN " + column.name + " " + column.sqlite).Error; err != nil {
				return err
			}
		}
	} else {
		parts := make([]string, 0, len(columns))
		args := []any{clause.Table{Name: "protocol_credentials"}}
		for _, column := range columns {
			if present[column.name] {
				continue
			}
			// These explicit derived definitions intentionally differ from the
			// model's IgnoreMigration behavior; do not use model AddColumn.
			parts = append(parts, "ADD COLUMN ? "+column.mysql)
			args = append(args, clause.Column{Name: column.name})
		}
		if len(parts) > 0 {
			if err := db.Exec("ALTER TABLE ? "+strings.Join(parts, ", "), args...).Error; err != nil {
				return err
			}
		}
	}

	indexes, err := db.Migrator().GetIndexes("protocol_credentials")
	if err != nil {
		return err
	}
	actual := map[string]string{}
	for _, index := range indexes {
		actual[index.Name()] = strings.Join(index.Columns(), ",")
	}
	for _, index := range []struct{ name, columns string }{
		{"idx_credential_principal_digest", "node_id,principal_digest"},
		{"idx_credential_secret_digest", "node_id,secret_principal_digest"},
		{"idx_credential_lookup_backfill", "node_id,principal_lookup_version,id"},
	} {
		if columns, exists := actual[index.name]; exists {
			if columns == index.columns {
				continue
			}
			if err := db.Migrator().DropIndex("protocol_credentials", index.name); err != nil {
				return err
			}
		}
		if err := db.Exec("CREATE INDEX " + index.name + " ON protocol_credentials(" + index.columns + ")").Error; err != nil {
			return err
		}
	}

	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: principalLookupSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}

// These derived fields are excluded from SQLite AutoMigrate. A complete local
// metadata read is required before invalidation or the first sequential ADD.
func principalLookupColumnNames(db *gorm.DB) (present map[string]bool, err error) {
	if !IsSQLite(db) {
		return mysqlColumnNames(db, "protocol_credentials")
	}
	rows, err := db.Raw("PRAGMA table_info(protocol_credentials)").Rows()
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	present = make(map[string]bool)
	for rows.Next() {
		var ordinal, required, primary int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&ordinal, &name, &kind, &required, &defaultValue, &primary); err != nil {
			return nil, err
		}
		present[strings.ToLower(name)] = true
	}
	return present, rows.Err()
}
