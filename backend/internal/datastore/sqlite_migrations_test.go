package datastore

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
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
	for _, column := range []string{"egress_protocol", "egress_config"} {
		if !db.Migrator().HasColumn("protocol_endpoints", column) {
			t.Errorf("SQLite protocol_endpoints is missing %q", column)
		}
	}
	if err := ReconcileTrafficReadSchema(db); err != nil {
		t.Fatalf("ReconcileTrafficReadSchema() error = %v", err)
	}
	for _, index := range trafficReadIndexes {
		if !db.Migrator().HasIndex(index.table, index.name) {
			t.Errorf("SQLite schema is missing traffic read index %q", index.name)
		}
	}
	for table, index := range map[string]string{
		"flow_usages":          "idx_flow_usages_endpoint_active",
		"protocol_deployments": "idx_protocol_deployments_endpoint_latest",
	} {
		if !db.Migrator().HasIndex(table, index) {
			t.Errorf("SQLite schema is missing statistics index %q", index)
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

func TestSQLiteJobTimeoutUpgradePreservesLegacyRunsAndIsRepeatable(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE job_runs DROP COLUMN timeout_ms").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = '0008_job_timeouts.up.sql'").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO job_runs(id,owner,`key`,handler,resource,payload,fingerprint,state,not_before,created_at,token,worker,schedule_id,execution_group) VALUES ('legacy','system','old','test','','{}','old','queued',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,'','','','')").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := RunMigrations(db); err != nil {
			t.Fatal(err)
		}
	}
	var row struct {
		ID        string
		TimeoutMS int64
	}
	if err := db.Table("job_runs").Where("id = ?", "legacy").Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.ID != "legacy" || row.TimeoutMS != 0 {
		t.Fatal(row)
	}
}

func TestSQLiteJobAttemptRetryUpgradePreservesFirstAttemptAndIsRepeatable(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "retry-upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO job_attempts(token,run_id,attempt_number,worker,state,started_at,expires_at)
VALUES ('legacy-token','legacy-run',1,'legacy-worker','failed',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`).Error; err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE legacy_job_attempts (token TEXT PRIMARY KEY, run_id TEXT NOT NULL UNIQUE, worker TEXT NOT NULL, state TEXT NOT NULL, started_at DATETIME NOT NULL, expires_at DATETIME NOT NULL, finished_at DATETIME)`,
		`INSERT INTO legacy_job_attempts(token,run_id,worker,state,started_at,expires_at,finished_at) SELECT token,run_id,worker,state,started_at,expires_at,finished_at FROM job_attempts`,
		`DROP TABLE job_attempts`,
		`ALTER TABLE legacy_job_attempts RENAME TO job_attempts`,
		`ALTER TABLE job_runs DROP COLUMN retry_backoff_ms`,
		`ALTER TABLE job_runs DROP COLUMN max_attempts`,
		`ALTER TABLE job_runs DROP COLUMN attempt_count`,
		`ALTER TABLE job_schedules DROP COLUMN retry_backoff_ms`,
		`ALTER TABLE job_schedules DROP COLUMN max_attempts`,
		`DELETE FROM schema_migrations WHERE version = '0014_job_attempt_retries.up.sql'`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := RunMigrations(db); err != nil {
			t.Fatal(err)
		}
	}
	var attempt struct {
		Token  string
		RunID  string
		Number int `gorm:"column:attempt_number"`
	}
	if err := db.Table("job_attempts").Where("token = ?", "legacy-token").Take(&attempt).Error; err != nil {
		t.Fatal(err)
	}
	if attempt.RunID != "legacy-run" || attempt.Number != 1 {
		t.Fatal(attempt)
	}
	for _, column := range []string{"attempt_count", "max_attempts", "retry_backoff_ms"} {
		if !db.Migrator().HasColumn("job_runs", column) {
			t.Fatalf("job_runs missing %s", column)
		}
	}
}

func TestSQLiteJobPlanningUpgradeBackfillsLanesAndIsRepeatable(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "planning-upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`DROP INDEX job_schedule_planned`,
		`DROP INDEX job_dispatch_ready`,
		`DROP TABLE job_dispatch_lanes`,
		`ALTER TABLE job_runs DROP COLUMN planned_at`,
		`ALTER TABLE job_runs DROP COLUMN dispatch_lane`,
		`ALTER TABLE job_schedules DROP COLUMN missed_runs`,
		`ALTER TABLE job_schedules DROP COLUMN anchor_at`,
		`ALTER TABLE job_schedules DROP COLUMN dispatch_lane`,
		`ALTER TABLE job_schedules DROP COLUMN misfire_policy`,
		`ALTER TABLE job_schedules DROP COLUMN timezone`,
		`ALTER TABLE job_schedules DROP COLUMN execution_group`,
		`ALTER TABLE job_execution_budget DROP COLUMN dispatch_virtual_time`,
		`DELETE FROM schema_migrations WHERE version = '0015_job_schedule_planning.up.sql'`,
		`INSERT INTO job_runs(timeout_ms,attempt_count,max_attempts,retry_backoff_ms,execution_group,id,owner,` + "`key`" + `,handler,resource,payload,fingerprint,state,not_before,created_at,token,worker,schedule_id) VALUES (0,0,1,0,'external','legacy-run','plugin:legacy','legacy','plugin:legacy:sync','','{}','legacy','queued',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,'','','')`,
		`INSERT INTO job_schedules(id,owner,name,handler,resource,revision,interval_ms,timeout_ms,max_attempts,retry_backoff_ms,next_at,sequence,run_id,runs,failures,state,last_state) VALUES ('plugin:legacy:sync','plugin:legacy','legacy','plugin:legacy:sync','','1',60000,1000,1,0,CURRENT_TIMESTAMP,0,'',0,0,'','')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := RunMigrations(db); err != nil {
			t.Fatal(err)
		}
	}
	var runLane, scheduleLane string
	if err := db.Table("job_runs").Select("dispatch_lane").Where("id = 'legacy-run'").Scan(&runLane).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("job_schedules").Select("dispatch_lane").Where("id = 'plugin:legacy:sync'").Scan(&scheduleLane).Error; err != nil {
		t.Fatal(err)
	}
	if runLane != "plugin:legacy" || scheduleLane != "plugin:legacy" {
		t.Fatal(runLane, scheduleLane)
	}
	for _, column := range []string{"dispatch_lane", "planned_at"} {
		if !db.Migrator().HasColumn("job_runs", column) {
			t.Fatalf("job_runs missing %s", column)
		}
	}
	if !db.Migrator().HasTable("job_dispatch_lanes") || !db.Migrator().HasIndex("job_runs", "job_schedule_planned") {
		t.Fatal("planning migration inventory is incomplete")
	}
}

func TestSQLiteEndpointUsageUpgradeBackfillsLedgerAndIsRepeatable(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "endpoint-usage-upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TrafficRecord{ProtocolEndpointID: 7, ReportID: "legacy", Nonce: "legacy", UsedBytes: 41, RawBytes: 43, At: time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM protocol_endpoint_usage_daily").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = '0016_protocol_endpoint_usage_daily.up.sql'").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := RunMigrations(db); err != nil {
			t.Fatal(err)
		}
	}
	var row model.ProtocolEndpointUsageDaily
	if err := db.First(&row, "protocol_endpoint_id = ?", 7).Error; err != nil {
		t.Fatal(err)
	}
	if row.UsedBytes != 41 || row.RawBytes != 43 || row.RecordCount != 1 || !strings.HasPrefix(row.UsageDate, "2026-09-17") {
		t.Fatal(row)
	}
}
