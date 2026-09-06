package datastore

import (
	"database/sql"
	"strings"

	"gorm.io/gorm"
)

const sqliteLedgerStateDDL = "CREATE TABLE traffic_ledger_revision (id INTEGER PRIMARY KEY CHECK (id = 1), revision INTEGER NOT NULL, instance TEXT NOT NULL DEFAULT (lower(hex(randomblob(16)))))"

func sqliteLedgerTriggers() map[string]string {
	result := make(map[string]string, 4)
	for name, event := range map[string]string{
		"traffic_ledger_revision_update": "UPDATE",
		"traffic_ledger_revision_delete": "DELETE",
		"traffic_ledger_revision_insert": "INSERT",
	} {
		when := ""
		if event == "INSERT" {
			when = " WHEN NEW.id < (SELECT MAX(id) FROM traffic_records)"
		}
		result[name] = "CREATE TRIGGER " + name + " AFTER " + event + " ON traffic_records" + when +
			" BEGIN UPDATE traffic_ledger_revision SET revision = revision + 1 WHERE id = 1; END"
	}
	// REPLACE may suppress DELETE triggers. Explicit replacement of the latest
	// ID would otherwise leave both the high-water mark and row count unchanged.
	result["traffic_ledger_revision_replace"] = "CREATE TRIGGER traffic_ledger_revision_replace BEFORE INSERT ON traffic_records" +
		" WHEN EXISTS (SELECT 1 FROM traffic_records WHERE id = NEW.id)" +
		" BEGIN UPDATE traffic_ledger_revision SET revision = revision + 1 WHERE id = 1; END"
	return result
}

// Ordinary append-only inserts do not write the revision row. Mutations of an
// existing prefix, including explicit insertion below the high-water mark,
// invalidate read projections in the same transaction. Rollback also rolls back
// invalidation; other processes and SQL clients cannot bypass this boundary.
func reconcileSQLiteLedgerRevision(db *gorm.DB) error {
	if !db.Migrator().HasTable("traffic_records") {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(strings.Replace(sqliteLedgerStateDDL, "CREATE TABLE ", "CREATE TABLE IF NOT EXISTS ", 1)).Error; err != nil {
			return err
		}
		if err := tx.Exec("INSERT OR IGNORE INTO traffic_ledger_revision(id, revision) VALUES (1, 0)").Error; err != nil {
			return err
		}
		for _, ddl := range sqliteLedgerTriggers() {
			if err := tx.Exec(strings.Replace(ddl, "CREATE TRIGGER ", "CREATE TRIGGER IF NOT EXISTS ", 1)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

type SQLiteLedgerVersion struct {
	Schema   int64
	Revision int64
	Instance string
	MaxID    sql.NullInt64
	Rows     int64
}

// Call inside the same read transaction as the projection query. If schema
// maintenance removes/changes the invalidation contract, callers must use a
// complete SQL read instead of trusting an incremental snapshot.
func ReadSQLiteLedgerVersion(tx *gorm.DB) (SQLiteLedgerVersion, bool, error) {
	var version SQLiteLedgerVersion
	if err := tx.Raw("PRAGMA schema_version").Scan(&version.Schema).Error; err != nil {
		return version, false, err
	}
	expected := sqliteLedgerTriggers()
	expected["traffic_ledger_revision"] = sqliteLedgerStateDDL
	names := make([]string, 0, len(expected))
	for name := range expected {
		names = append(names, name)
	}
	var objects []struct{ Name, SQL string }
	if err := tx.Raw("SELECT name, sql FROM sqlite_master WHERE name IN ?", names).Scan(&objects).Error; err != nil {
		return version, false, err
	}
	if len(objects) != len(expected) {
		return version, false, nil
	}
	for _, object := range objects {
		if strings.TrimSpace(object.SQL) != expected[object.Name] {
			return version, false, nil
		}
	}
	var row struct {
		Revision int64
		Instance string
	}
	read := tx.Raw("SELECT revision, instance FROM traffic_ledger_revision WHERE id = 1").Scan(&row)
	if read.Error != nil || read.RowsAffected != 1 {
		return version, false, read.Error
	}
	version.Revision = row.Revision
	version.Instance = row.Instance
	if err := tx.Raw("SELECT MAX(id) FROM traffic_records").Row().Scan(&version.MaxID); err != nil {
		return version, false, err
	}
	// Keep COUNT(*) separate from MAX(id): SQLite can count B-tree cells without
	// interpreting every record. This catches implicit deletions from REPLACE.
	err := tx.Raw("SELECT COUNT(*) FROM traffic_records").Row().Scan(&version.Rows)
	return version, err == nil, err
}
