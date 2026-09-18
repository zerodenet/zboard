package application

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

func schemaFixture(t *testing.T, driver string) *gorm.DB {
	t.Helper()
	source := filepath.Join(t.TempDir(), "schema.db")
	if driver == datastore.DriverMySQL {
		source = os.Getenv("ZBOARD_TEST_MYSQL_DSN")
		if source == "" {
			t.Skip("ZBOARD_TEST_MYSQL_DSN is required for MySQL integration")
		}
		config, err := mysqlconfig.ParseDSN(source)
		if err != nil {
			t.Fatal(err)
		}
		config.DBName = ""
		config.ParseTime = true
		config.Loc = time.UTC
		admin, err := sql.Open("mysql", config.FormatDSN())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { admin.Close() })
		name := "zboard_schema_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if _, err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := admin.Exec("DROP DATABASE `" + name + "`"); err != nil {
				t.Error(err)
			}
		})
		config.DBName = name
		source = config.FormatDSN()
	}
	db, err := datastore.OpenWithDriver(driver, source)
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	return db
}

func TestPrepareDatabaseSchemaFreshAndRepeatPreserveProjectionData(t *testing.T) {
	for _, driver := range []string{datastore.DriverSQLite, datastore.DriverMySQL} {
		t.Run(driver, func(t *testing.T) {
			db := schemaFixture(t, driver)
			if err := PrepareDatabaseSchema(db); err != nil {
				t.Fatal(err)
			}
			tables, err := datastore.MigrationTables(db)
			if err != nil {
				t.Fatal(err)
			}
			for _, table := range tables {
				if !db.Migrator().HasTable(table) {
					t.Fatalf("missing migration data table %s", table)
				}
			}
			if !db.Migrator().HasIndex("principal_flow_observations", "idx_principal_flow_observation_user_timeline") {
				t.Fatal("projection read index missing")
			}
			if !db.Migrator().HasColumn("job_runs", "timeout_ms") {
				t.Fatal("job timeout migration missing")
			}
			now := time.Now().UTC().Truncate(time.Millisecond)
			observation := meteringstore.PrincipalFlowObservation{NodeID: 1, PrincipalKey: "principal", CoreInstanceID: "core", EventID: "historical", UserID: 1, ActiveFlows: 7, ObservedAt: now, CreatedAt: now}
			if err := db.Create(&observation).Error; err != nil {
				t.Fatal(err)
			}
			var before int64
			if err := db.Table("schema_migrations").Count(&before).Error; err != nil {
				t.Fatal(err)
			}
			if err := PrepareDatabaseSchema(db); err != nil {
				t.Fatal(err)
			}
			var retained meteringstore.PrincipalFlowObservation
			if err := db.First(&retained, observation.ID).Error; err != nil || retained.ActiveFlows != 7 || retained.EventID != "historical" {
				t.Fatal("schema retry changed data", retained, err)
			}
			var after int64
			if err := db.Table("schema_migrations").Count(&after).Error; err != nil || after != before {
				t.Fatal("schema retry changed migration history", before, after, err)
			}
		})
	}
}

func TestPrepareDatabaseSchemaUpgradesPreviouslyMigratedDatabase(t *testing.T) {
	db := schemaFixture(t, datastore.DriverMySQL)
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	// This is the old migrate-only result: it omits route-installed projections.
	if db.Migrator().HasTable("principal_flow_currents") {
		t.Fatal("fixture no longer models old migrate-only boundary")
	}
	if err := PrepareDatabaseSchema(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"principal_flow_currents", "principal_flow_scope_observations", "fair_use_policies", "subscription_fair_use_events"} {
		if !db.Migrator().HasTable(table) {
			t.Fatal("upgrade omitted", table)
		}
	}
}

func TestDatabaseCopyPreservesProjectionRowsAndRejectsMissingDestination(t *testing.T) {
	for _, driver := range []string{datastore.DriverSQLite, datastore.DriverMySQL} {
		t.Run(driver, func(t *testing.T) {
			source := schemaFixture(t, datastore.DriverSQLite)
			target := schemaFixture(t, driver)
			for _, db := range []*gorm.DB{source, target} {
				if err := PrepareDatabaseSchema(db); err != nil {
					t.Fatal(err)
				}
			}
			now := time.Now().UTC().Truncate(time.Millisecond)
			row := meteringstore.PrincipalFlowObservation{NodeID: 1, PrincipalKey: "copied", CoreInstanceID: "core", EventID: "history", UserID: 1, ActiveFlows: 9, ObservedAt: now, CreatedAt: now}
			if err := source.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			// Schema preparation seeds the host budget; the real copy path clears
			// the empty destination's schema defaults before copying source data.
			clearDefaults := func() {
				t.Helper()
				if err := ClearDatabaseCopyDestination(target); err != nil {
					t.Fatal(err)
				}
			}
			clearDefaults()
			// Remove a later table: failure must roll back already-copied history.
			if err := target.Migrator().DropTable("principal_flow_scope_observations"); err != nil {
				t.Fatal(err)
			}
			if err := target.Transaction(func(tx *gorm.DB) error { return CopyDatabaseData(source, tx) }); err == nil || !strings.Contains(err.Error(), "missing source table principal_flow_scope_observations") {
				t.Fatal("copy silently omitted missing destination", err)
			}
			var count int64
			if err := target.Model(&meteringstore.PrincipalFlowObservation{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("partial copy committed", count, err)
			}
			if err := PrepareDatabaseSchema(target); err != nil {
				t.Fatal(err)
			}
			clearDefaults()
			if err := target.Transaction(func(tx *gorm.DB) error { return CopyDatabaseData(source, tx) }); err != nil {
				t.Fatal(err)
			}
			var copied meteringstore.PrincipalFlowObservation
			if err := target.First(&copied, row.ID).Error; err != nil || copied.EventID != row.EventID || copied.ActiveFlows != row.ActiveFlows || !copied.ObservedAt.Equal(row.ObservedAt) {
				t.Fatal("projection history changed", copied, err)
			}
		})
	}
}
