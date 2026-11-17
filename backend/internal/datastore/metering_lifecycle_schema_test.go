package datastore_test

import (
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"strings"
	"testing"
)

func TestSQLiteMeteringLifecycleSchemaResumesAndRefusesLostAuthority(t *testing.T) {
	assertLifecycleSchema(t, false)
}
func TestMySQLMeteringLifecycleSchemaResumesAndRefusesLostAuthority(t *testing.T) {
	assertLifecycleSchema(t, true)
}
func assertLifecycleSchema(t *testing.T, mysql bool) {
	db := diagnosticSchemaDB(t, mysql)
	// Interrupted additive preparation: remove only empty, not-yet-owned tables.
	for _, v := range []string{"0088_metering_cold_identities.up.sql", "0087_metering_ledger_retention.up.sql", "0086_metering_settlement_results.up.sql"} {
		if e := db.Exec("DELETE FROM schema_migrations WHERE version=?", v).Error; e != nil {
			t.Fatal(e)
		}
	}
	for _, table := range []string{"metering_cold_roots", "metering_cold_progress", "metering_identity_profiles", "metering_cold_blocks", "metering_ledger_targets", "metering_nonce_receipts", "metering_ledger_retention_progress", "metering_settlement_results", "metering_settlement_progress"} {
		if e := db.Migrator().DropTable(table); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 2; i++ {
		if e := datastore.RunMigrations(db); e != nil {
			t.Fatal(e)
		}
	}
	if e := db.Exec("DELETE FROM metering_identity_profiles WHERE kind=1").Error; e != nil {
		t.Fatal(e)
	}
	if e := datastore.RunMigrations(db); e == nil || !strings.Contains(e.Error(), "profile") {
		t.Fatal("lost authority was re-created", e)
	}
	var n int64
	db.Table("metering_identity_profiles").Count(&n)
	if n != 2 {
		t.Fatal("startup repaired a lost authority instead of rejecting", n)
	}
}
