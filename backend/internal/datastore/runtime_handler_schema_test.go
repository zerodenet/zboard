package datastore

import (
	"path/filepath"
	"testing"
)

func TestRuntimeHandlerIndexResumesCommittedDDLWithoutChangingHistory(t *testing.T) {
	db, err := OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	for _, ddl := range []string{
		"CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at DATETIME NOT NULL)",
		"CREATE TABLE job_runs (id TEXT PRIMARY KEY, owner TEXT, handler TEXT, finished_at DATETIME)",
		"INSERT INTO job_runs VALUES ('retained', 'system', 'sync', NULL)",
		"CREATE INDEX job_runtime_handler ON job_runs(owner,handler,finished_at)",
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := reconcileRuntimeHandlerIndex(db); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	db.Table("schema_migrations").Where("version=?", runtimeHandlerIndexVersion).Count(&count)
	if count != 1 {
		t.Fatalf("migration receipts=%d", count)
	}
	db.Table("job_runs").Where("id='retained'").Count(&count)
	if count != 1 {
		t.Fatal("retained task history changed")
	}
	if err := db.Exec("DROP INDEX job_runtime_handler").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE INDEX job_runtime_handler ON job_runs(handler)").Error; err != nil {
		t.Fatal(err)
	}
	if err := reconcileRuntimeHandlerIndex(db); err == nil {
		t.Fatal("incompatible index was accepted")
	}
}
