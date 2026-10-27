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

const coverageRuntimeVersion = "0068_coverage_runtime_identity.up.sql"

var coverageRuntimeTables = []string{"fair_use_node_coverage", "subscription_flow_start_events"}

// These are derived auxiliary tables, not settlement/nonce/cursor authority.
// Keep the existing auxiliary table recovery policy, then widen only their
// non-indexed opaque identity columns. No existing identity or window changes.
// Recheck after the migration loop as legacy startup reconciliation can restore
// a missing auxiliary table with its original VARCHAR(128) definition.
func reconcileCoverageRuntimeSchema(db *gorm.DB) error {
	if db == nil {
		return errors.New("coverage runtime identity requires database")
	}
	if !IsSQLite(db) {
		if err := ReconcileFairUseTelemetrySchema(db); err != nil {
			return err
		}
	}
	asset := coverageRuntimeVersion
	if IsSQLite(db) {
		asset = "sqlite/0068_coverage_runtime_identity.sql"
	}
	raw, err := migrations.Files.ReadFile(asset)
	if err != nil {
		return err
	}
	ddl, err := splitMigrationStatements(string(raw))
	if err != nil {
		return err
	}
	expected := 2
	if IsSQLite(db) {
		expected = 1
	}
	if len(ddl) != expected {
		return errors.New("coverage runtime migration inventory differs")
	}
	for i, table := range coverageRuntimeTables {
		kind, err := coverageRuntimeColumn(db, table)
		if err != nil {
			return err
		}
		if IsSQLite(db) {
			if kind != "text" {
				return fmt.Errorf("%s runtime identity must remain TEXT", table)
			}
			continue
		}
		if kind == "mediumtext" {
			continue
		}
		if kind != "varchar(128)" {
			return fmt.Errorf("%s runtime identity has unexpected type %s; inspect before migration", table, kind)
		}
		if err := db.Exec(ddl[i]).Error; err != nil {
			return fmt.Errorf("widen %s runtime identity: %w", table, err)
		}
		kind, err = coverageRuntimeColumn(db, table)
		if err != nil {
			return err
		}
		if kind != "mediumtext" {
			return fmt.Errorf("%s runtime identity widening was not confirmed", table)
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: coverageRuntimeVersion, AppliedAt: time.Now().UTC()}).Error
}

func coverageRuntimeColumn(db *gorm.DB, table string) (string, error) {
	// Only fixed owned table names enter metadata/DDL, never request input.
	if table != "fair_use_node_coverage" && table != "subscription_flow_start_events" {
		return "", errors.New("unexpected coverage identity table")
	}
	columns, _, err := projectionColumns(db, table)
	if err != nil {
		return "", err
	}
	c, ok := columns["core_instance_id"]
	if !ok || c.nullable {
		return "", fmt.Errorf("%s runtime identity column missing or nullable; inspect before migration", table)
	}
	return strings.ToLower(c.kind), nil
}

// Copy cannot silently omit recorded coverage history. Startup can rebuild a
// lost auxiliary table as explicitly unknown; copying a damaged recorded source
// instead requires restoration or an explicit operator recovery first.
func preflightCoverageRuntimeCopy(db *gorm.DB) error {
	tables, err := migrationTableSet(db)
	if err != nil {
		return err
	}
	var count int64
	if tables["schema_migrations"] {
		if err := db.Table("schema_migrations").Where("version = ?", coverageRuntimeVersion).Count(&count).Error; err != nil {
			return err
		}
	}
	for _, table := range coverageRuntimeTables {
		if !tables[table] {
			if count > 0 {
				return fmt.Errorf("recorded coverage runtime table %s missing; restore before copy", table)
			}
			continue
		}
		kind, err := coverageRuntimeColumn(db, table)
		if err != nil {
			return err
		}
		if kind != "text" && kind != "mediumtext" && kind != "varchar(128)" {
			return fmt.Errorf("unexpected copied %s runtime identity type %s", table, kind)
		}
		size := "OCTET_LENGTH(core_instance_id)"
		if IsSQLite(db) {
			size = "length(CAST(core_instance_id AS BLOB))"
		}
		var head struct{ Maximum int64 }
		if err := db.Table(table).Select("COALESCE(MAX(" + size + "),0) AS maximum").Take(&head).Error; err != nil {
			return err
		}
		if head.Maximum > auxiliaryRuntimeIdentityMaxBytes {
			return errors.New("copied coverage runtime body exceeds evidence budget")
		}

	}
	return nil
}

func requireCoverageRuntimeDestination(db *gorm.DB) error {
	for _, table := range coverageRuntimeTables {
		kind, err := coverageRuntimeColumn(db, table)
		if err != nil {
			return err
		}
		expected := "mediumtext"
		if IsSQLite(db) {
			expected = "text"
		}
		if kind != expected {
			return fmt.Errorf("copy destination %s runtime identity is not prepared", table)
		}
	}
	return nil
}

const auxiliaryRuntimeIdentityMaxBytes = 2 * 1024 * 1024
const auxiliaryRuntimeCopyBatchBytes = 4 * 1024 * 1024

func coverageRuntimeCopyRecord(table string, record map[string]interface{}) (int64, error) {
	if table != "fair_use_node_coverage" && table != "subscription_flow_start_events" {
		return 0, nil
	}
	raw, ok := record["core_instance_id"].(string)
	if !ok || len(raw) > auxiliaryRuntimeIdentityMaxBytes {
		return 0, errors.New("copied coverage runtime raw identity is invalid or exceeds evidence bound")
	}
	return int64(len(raw)), nil
}
