package datastore

import (
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
	missing := false
	for _, column := range []struct{ name, mysql, sqlite string }{
		{"principal_digest", "varchar(64) NOT NULL DEFAULT ''", "TEXT NOT NULL DEFAULT ''"},
		{"secret_principal_digest", "varchar(64) NOT NULL DEFAULT ''", "TEXT NOT NULL DEFAULT ''"},
		{"principal_lookup_version", "tinyint unsigned NOT NULL DEFAULT 0", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if db.Migrator().HasColumn("protocol_credentials", column.name) {
			continue
		}
		definition := column.mysql
		if IsSQLite(db) {
			definition = column.sqlite
		}
		if err := db.Exec("ALTER TABLE protocol_credentials ADD COLUMN " + column.name + " " + definition).Error; err != nil {
			return err
		}
		missing = true
	}
	if missing {
		if err := db.Exec("UPDATE protocol_credentials SET principal_lookup_version = 0 WHERE principal_lookup_version <> 0").Error; err != nil {
			return err
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
