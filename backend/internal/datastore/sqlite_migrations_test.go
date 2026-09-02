package datastore

import (
	"path/filepath"
	"testing"
)

func TestSQLiteMigrationsCreateCompleteApplicationInventory(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "zboard.db"))
	if err != nil {
		t.Fatalf("OpenWithDriver() error = %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB() error = %v", err)
	}
	defer sqlDB.Close()
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	tables, err := MigrationTables(db)
	if err != nil {
		t.Fatalf("MigrationTables() error = %v", err)
	}
	for _, table := range tables {
		if !db.Migrator().HasTable(table) {
			t.Errorf("SQLite schema is missing migration table %q", table)
		}
	}
	if !db.Migrator().HasTable("schema_migrations") {
		t.Error("SQLite schema is missing schema_migrations")
	}
	if err := ReconcileTrafficReadSchema(db); err != nil {
		t.Fatalf("ReconcileTrafficReadSchema() error = %v", err)
	}
	for _, index := range trafficReadIndexes {
		if !db.Migrator().HasIndex(index.table, index.name) {
			t.Errorf("SQLite schema is missing traffic read index %q", index.name)
		}
	}
}

func TestSQLiteMigrationInventoryHasNoDuplicates(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "inventory.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	tables, err := MigrationTables(db)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, table := range tables {
		if seen[table] {
			t.Fatalf("duplicate migration table %q", table)
		}
		seen[table] = true
	}
}
