package datastore_test

import (
	"reflect"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestSQLiteNativeSettlementPartialDDLRecoveryPreservesLegacyHistory(t *testing.T) {
	nativeSettlementRecovery(t, false)
}
func TestMySQLNativeSettlementPartialDDLRecoveryPreservesLegacyHistory(t *testing.T) {
	nativeSettlementRecovery(t, true)
}
func nativeSettlementRecovery(t *testing.T, mysql bool) {
	t.Helper()
	db := diagnosticSchemaDB(t, mysql)
	original := diagnosticSchemaSource(t, db, false)
	if err := db.First(&original, original.ID).Error; err != nil {
		t.Fatal(err)
	}
	// Some instant ALTERs may already be committed when a process stops. Resume
	// missing columns without rewriting the existing financial/source material.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version=?", "0078_native_minute_settlement.up.sql").Error; err != nil {
		t.Fatal(err)
	}
	ddl := "ALTER TABLE metering_usage_projection_pending DROP COLUMN delta_used_bytes"
	if mysql {
		ddl += ", ALGORITHM=INSTANT"
	}
	if err := db.Exec(ddl).Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	var retained model.TrafficRecord
	if err := db.First(&retained, original.ID).Error; err != nil || !reflect.DeepEqual(original, retained) {
		t.Fatal("upgrade rewrote legacy financial history", err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("second startup", err)
	}
	if mysql {
		if err := db.Exec("ALTER TABLE traffic_records ALTER COLUMN record_count SET DEFAULT 0").Error; err != nil {
			t.Fatal(err)
		}
	} else {
		if err := db.Exec("ALTER TABLE traffic_records DROP COLUMN record_count").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("ALTER TABLE traffic_records ADD COLUMN record_count integer NOT NULL DEFAULT 0").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := datastore.RunMigrations(db); err == nil {
		t.Fatal("incompatible count default admitted")
	}
}
