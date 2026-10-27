package datastore_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

const coverageRuntimeVersion = "0068_coverage_runtime_identity.up.sql"

func TestSQLiteCoverageRuntimeMigrationAndCopy(t *testing.T) { assertCoverageRuntimeSchema(t, false) }
func TestMySQLCoverageRuntimeMigrationAndCopy(t *testing.T)  { assertCoverageRuntimeSchema(t, true) }
func assertCoverageRuntimeSchema(t *testing.T, mysql bool) {
	db := diagnosticSchemaDB(t, mysql)
	at := time.Now().UTC().Truncate(time.Millisecond)
	short := meteringstore.CoverageRecord{NodeID: 73, CoreInstanceID: "existing-short", LastSequence: 8, LastEventID: "existing-event", ContinuousSinceAt: at.Add(-time.Hour), LastReceivedAt: at, LastEventOccurredAt: at, GapCount: 2, UpdatedAt: at}
	if err := db.Create(&short).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", coverageRuntimeVersion).Error; err != nil {
		t.Fatal(err)
	}
	if mysql {
		// Recreate the published legacy width, including an interrupted migration
		// where only the first ALTER completed. Existing short history is genuine.
		if err := db.Exec("ALTER TABLE subscription_flow_start_events MODIFY COLUMN core_instance_id varchar(128) NOT NULL DEFAULT ''").Error; err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal(err)
		}
	}
	var retained meteringstore.CoverageRecord
	if err := db.First(&retained, "node_id = ?", short.NodeID).Error; err != nil || !reflect.DeepEqual(short, retained) {
		t.Fatal("upgrade changed established coverage window", retained, err)
	}
	long := short
	long.NodeID = 74
	long.CoreInstanceID = strings.Repeat("runtime-", 45)
	if err := db.Create(&long).Error; err != nil {
		t.Fatal(err)
	}
	target := diagnosticSchemaDB(t, mysql)
	if err := datastore.WithCopyDestination(t.Context(), target, func(tx *gorm.DB) error {
		tables, err := datastore.MigrationTables(tx)
		if err != nil {
			return err
		}
		if err := datastore.ClearCopyDestinationRows(tx, tables); err != nil {
			return err
		}
		return datastore.CopyApplicationData(db, tx)
	}); err != nil {
		t.Fatal(err)
	}
	var copied meteringstore.CoverageRecord
	if err := target.First(&copied, "node_id = ?", long.NodeID).Error; err != nil || !reflect.DeepEqual(long, copied) {
		t.Fatal("copy changed long identity/window", copied, err)
	}
	// Recorded missing columns must not be guessed from event IDs or sequences.
	if err := db.Migrator().DropColumn("fair_use_node_coverage", "core_instance_id"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil {
		t.Fatal("missing runtime identity column silently invented")
	}
	if err := datastore.CopyApplicationData(db, target); err == nil {
		t.Fatal("copy accepted missing recorded identity column")
	}
	if db.Migrator().HasColumn("fair_use_node_coverage", "core_instance_id") {
		t.Fatal("rejection defaulted existing coverage to unknown generation")
	}
}
func TestSQLiteCoverageRuntimeMissingTableCopyRefuses(t *testing.T) {
	assertCoverageRuntimeMissingTable(t, false)
}
func TestMySQLCoverageRuntimeMissingTableCopyRefuses(t *testing.T) {
	assertCoverageRuntimeMissingTable(t, true)
}
func assertCoverageRuntimeMissingTable(t *testing.T, mysql bool) {
	db := diagnosticSchemaDB(t, mysql)
	target := diagnosticSchemaDB(t, mysql)
	if err := db.Migrator().DropTable("fair_use_node_coverage"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.CopyApplicationData(db, target); err == nil || !strings.Contains(err.Error(), "recorded coverage runtime table") {
		t.Fatal("copy omitted lost coverage table", err)
	}
	// Existing operational recovery recreates derived coverage as empty/unknown,
	// never as confirmed zero activity; it must also reapply the long identity type.
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Millisecond)
	row := meteringstore.CoverageRecord{NodeID: 92, CoreInstanceID: strings.Repeat("recovered-", 40), LastSequence: 1, LastEventID: "after-recovery", ContinuousSinceAt: at, LastReceivedAt: at, LastEventOccurredAt: at, UpdatedAt: at}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal("recovered table retained legacy128 width", err)
	}
}
