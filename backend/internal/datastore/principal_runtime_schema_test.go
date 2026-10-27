package datastore_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

const principalRuntimeVersion = "0069_principal_runtime_identity.up.sql"
const principalRuntimeIndex = "uk_principal_flow_observation_runtime_revision"

func TestSQLitePrincipalRuntimeUpgradePartialDDLAndLegacyCopy(t *testing.T) {
	assertPrincipalRuntimeUpgrade(t, false)
}
func TestMySQLPrincipalRuntimeUpgradePartialDDLAndLegacyCopy(t *testing.T) {
	assertPrincipalRuntimeUpgrade(t, true)
}
func assertPrincipalRuntimeUpgrade(t *testing.T, mysql bool) {
	db := diagnosticSchemaDB(t, mysql)
	at := time.Now().UTC().Truncate(time.Millisecond)
	old := meteringstore.PrincipalFlowObservation{NodeID: 73, CoreInstanceID: "existing-short", CoreInstanceSHA256: metering.RuntimeIdentityDigest("existing-short"), SessionRegistryRevision: 5, EventID: "existing-short-observation", Sequence: 8, PrincipalKey: "existing-short-principal", ActiveFlows: 7, ObservedAt: at, CreatedAt: at}
	generation := meteringstore.PrincipalFlowNodeGeneration{NodeID: 73, CoreInstanceID: old.CoreInstanceID, StartedAt: at.Add(-time.Hour), UpdatedAt: at}
	current := meteringstore.PrincipalFlowCurrent{NodeID: 73, PrincipalKey: old.PrincipalKey, CoreInstanceID: old.CoreInstanceID, SessionRegistryRevision: 5, ActiveFlows: 7, ObservedAt: at, UpdatedAt: at}
	for _, row := range []any{&old, &generation, &current} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	principalRuntimeLegacyDDL(t, db, mysql)
	// Before upgrading the source, a prepared destination must derive only its
	// copied digest. The legacy source itself stays untouched and remains usable.
	target := diagnosticSchemaDB(t, mysql)
	principalRuntimeCopy(t, db, target)
	var copied meteringstore.PrincipalFlowObservation
	if err := target.First(&copied, old.ID).Error; err != nil || !reflect.DeepEqual(old, copied) {
		t.Fatal("legacy copy changed historical observation", copied, err)
	}
	if db.Migrator().HasColumn("principal_flow_observations", "core_instance_sha256") {
		t.Fatal("legacy copy mutated source schema")
	}
	// Interrupt after ADD COLUMN, including one completed per-row backfill. The
	// original unique key is still present; rerun must verify instead of erase.
	definition := "char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT ''"
	if !mysql {
		definition = "TEXT NOT NULL DEFAULT ''"
	}
	if err := db.Exec("ALTER TABLE principal_flow_observations ADD COLUMN core_instance_sha256 " + definition).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("principal_flow_observations").Where("id = ?", old.ID).Update("core_instance_sha256", old.CoreInstanceSHA256).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal(err)
		}
	}
	var retained meteringstore.PrincipalFlowObservation
	if err := db.First(&retained, old.ID).Error; err != nil || !reflect.DeepEqual(old, retained) {
		t.Fatal("upgrade changed existing short identity or activity", retained, err)
	}
	var gotGeneration meteringstore.PrincipalFlowNodeGeneration
	var gotCurrent meteringstore.PrincipalFlowCurrent
	if err := db.First(&gotGeneration, "node_id = ?", generation.NodeID).Error; err != nil || !reflect.DeepEqual(generation, gotGeneration) {
		t.Fatal("upgrade reset generation start", err)
	}
	if err := db.First(&gotCurrent, "node_id = ? AND principal_key = ?", current.NodeID, current.PrincipalKey).Error; err != nil || !reflect.DeepEqual(current, gotCurrent) {
		t.Fatal("upgrade rewrote current window", err)
	}
	long := old
	long.ID = 0
	long.CoreInstanceID = strings.Repeat("same-prefix-", 25) + "A"
	long.CoreInstanceSHA256 = metering.RuntimeIdentityDigest(long.CoreInstanceID)
	long.EventID = "long-observation"
	if err := db.Create(&long).Error; err != nil {
		t.Fatal(err)
	}
	distinct := long
	distinct.ID = 0
	distinct.CoreInstanceID = strings.Repeat("same-prefix-", 25) + "B"
	distinct.CoreInstanceSHA256 = metering.RuntimeIdentityDigest(distinct.CoreInstanceID)
	distinct.EventID = "distinct-long-observation"
	if err := db.Create(&distinct).Error; err != nil {
		t.Fatal("same raw prefix incorrectly imposed identity uniqueness", err)
	}
	principalRuntimeCopy(t, db, target)
	var gotLong meteringstore.PrincipalFlowObservation
	if err := target.First(&gotLong, long.ID).Error; err != nil || !reflect.DeepEqual(long, gotLong) {
		t.Fatal("prepared copy lost complete raw/digest pair", err)
	}
}

func principalRuntimeLegacyDDL(t *testing.T, db *gorm.DB, mysql bool) {
	t.Helper()
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", principalRuntimeVersion).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropIndex("principal_flow_observations", principalRuntimeIndex); err != nil {
		t.Fatal(err)
	}
	if mysql {
		for _, ddl := range []string{
			"ALTER TABLE principal_flow_observations MODIFY COLUMN core_instance_id varchar(128) NOT NULL, ADD UNIQUE INDEX uk_principal_flow_observation_revision(node_id,core_instance_id,session_registry_revision,principal_key)",
			"ALTER TABLE principal_flow_node_generations DROP INDEX idx_principal_flow_generation_instance, MODIFY COLUMN core_instance_id varchar(128) NOT NULL, ADD INDEX idx_principal_flow_generation_instance(core_instance_id)",
			"ALTER TABLE principal_flow_currents DROP INDEX idx_principal_flow_current_instance, MODIFY COLUMN core_instance_id varchar(128) NOT NULL, ADD INDEX idx_principal_flow_current_instance(node_id,core_instance_id)",
			"ALTER TABLE principal_flow_scope_observations MODIFY COLUMN core_instance_id varchar(128) NOT NULL DEFAULT ''",
		} {
			if err := db.Exec(ddl).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := db.Migrator().DropColumn("principal_flow_observations", "core_instance_sha256"); err != nil {
		t.Fatal(err)
	}
}

func principalRuntimeCopy(t *testing.T, source, target *gorm.DB) {
	t.Helper()
	if err := datastore.WithCopyDestination(t.Context(), target, func(tx *gorm.DB) error {
		tables, err := datastore.MigrationTables(tx)
		if err != nil {
			return err
		}
		if err := datastore.ClearCopyDestinationRows(tx, tables); err != nil {
			return err
		}
		return datastore.CopyApplicationData(source, tx)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSQLitePrincipalRuntimeRejectsRecordedMissingIdentityAndWeakIndex(t *testing.T) {
	assertPrincipalRuntimeGuards(t, false)
}
func TestMySQLPrincipalRuntimeRejectsRecordedMissingIdentityAndWeakIndex(t *testing.T) {
	assertPrincipalRuntimeGuards(t, true)
}
func assertPrincipalRuntimeGuards(t *testing.T, mysql bool) {
	for _, damage := range []string{"column", "index", "table", "weak-index"} {
		t.Run(damage, func(t *testing.T) {
			db := diagnosticSchemaDB(t, mysql)
			target := diagnosticSchemaDB(t, mysql)
			switch damage {
			case "column":
				if err := db.Migrator().DropIndex("principal_flow_observations", principalRuntimeIndex); err != nil {
					t.Fatal(err)
				}
				if err := db.Migrator().DropColumn("principal_flow_observations", "core_instance_sha256"); err != nil {
					t.Fatal(err)
				}
			case "index":
				if err := db.Migrator().DropIndex("principal_flow_observations", principalRuntimeIndex); err != nil {
					t.Fatal(err)
				}
			case "table":
				if err := db.Migrator().DropTable("principal_flow_observations"); err != nil {
					t.Fatal(err)
				}
			case "weak-index":
				if err := db.Migrator().DropIndex("principal_flow_observations", principalRuntimeIndex); err != nil {
					t.Fatal(err)
				}
				ddl := "CREATE UNIQUE INDEX " + principalRuntimeIndex + " ON principal_flow_observations(node_id,core_instance_sha256,session_registry_revision,principal_key) WHERE active_flows > 0"
				if mysql {
					ddl = "CREATE UNIQUE INDEX " + principalRuntimeIndex + " ON principal_flow_observations(node_id,core_instance_sha256(8),session_registry_revision,principal_key)"
				}
				if err := db.Exec(ddl).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := datastore.RunMigrations(db); err == nil {
				t.Fatal("damaged recorded identity evidence was auto-recreated")
			}
			if err := datastore.CopyApplicationData(db, target); err == nil {
				t.Fatal("copy adopted damaged recorded identity evidence")
			}
		})
	}
}

func TestSQLitePrincipalRuntimePartialDigestMismatchPreservesOriginal(t *testing.T) {
	assertPrincipalRuntimePartialMismatch(t, false)
}
func TestMySQLPrincipalRuntimePartialDigestMismatchPreservesOriginal(t *testing.T) {
	assertPrincipalRuntimePartialMismatch(t, true)
}
func assertPrincipalRuntimePartialMismatch(t *testing.T, mysql bool) {
	db := diagnosticSchemaDB(t, mysql)
	at := time.Now().UTC().Truncate(time.Millisecond)
	row := meteringstore.PrincipalFlowObservation{NodeID: 31, CoreInstanceID: "original-runtime", CoreInstanceSHA256: metering.RuntimeIdentityDigest("original-runtime"), SessionRegistryRevision: 1, EventID: "original-observation", PrincipalKey: "original-principal", ActiveFlows: 7, ObservedAt: at, CreatedAt: at}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	// An interrupted owned migration may contain a nonempty digest. Verify it;
	// never silently recompute a conflicting value and claim original authority.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", principalRuntimeVersion).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&row).Update("core_instance_sha256", strings.Repeat("f", 64)).Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "raw/digest mismatch") {
		t.Fatal("partial corrupt digest silently repaired", err)
	}
	var got meteringstore.PrincipalFlowObservation
	if err := db.First(&got, row.ID).Error; err != nil || got.CoreInstanceID != row.CoreInstanceID || got.CoreInstanceSHA256 != strings.Repeat("f", 64) || got.ActiveFlows != row.ActiveFlows {
		t.Fatal("refusal changed retained original", got, err)
	}
	var receipts int64
	if err := db.Table("schema_migrations").Where("version = ?", principalRuntimeVersion).Count(&receipts).Error; err != nil || receipts != 0 {
		t.Fatal("failed validation recorded as completed", receipts, err)
	}
}
