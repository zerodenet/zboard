package datastore_test

import (
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"reflect"
	"strings"
	"testing"
	"time"
)

const protocolDeletionVersion = "0075_protocol_deletion.up.sql"

func TestSQLiteProtocolDeletionUpgradeRecoveryAndCopy(t *testing.T) {
	protocolDeletionUpgrade(t, false)
}
func TestMySQLProtocolDeletionUpgradeRecoveryAndCopy(t *testing.T) { protocolDeletionUpgrade(t, true) }
func protocolDeletionUpgrade(t *testing.T, mysql bool) {
	db := diagnosticSchemaDB(t, mysql)
	row := diagnosticSchemaSource(t, db, false)
	var ledger model.TrafficRecord
	if err := db.First(&ledger, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	// Simulate actual pre-deletion schema; startup must add no historical state.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", protocolDeletionVersion).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropIndex("protocol_endpoints", "idx_protocol_endpoints_deletion"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE protocol_endpoints DROP COLUMN deleted_at").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("legacy upgrade", err)
	}
	var endpoint model.ProtocolEndpoint
	if err := db.Unscoped().First(&endpoint, row.ProtocolEndpointID).Error; err != nil || endpoint.DeletedAt.Valid {
		t.Fatal("upgrade inferred deletion", endpoint, err)
	}
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if err := db.Model(&endpoint).Updates(map[string]any{"deleted_at": at, "is_active": false}).Error; err != nil {
		t.Fatal(err)
	}
	endpoint = model.ProtocolEndpoint{}
	if err := db.Unscoped().First(&endpoint, row.ProtocolEndpointID).Error; err != nil {
		t.Fatal(err)
	}
	// Interrupted DDL can resume without rewriting a single terminal deletion timestamp.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", protocolDeletionVersion).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal("interrupted migration", err)
		}
	}
	var current model.ProtocolEndpoint
	if err := db.Unscoped().First(&current, endpoint.ID).Error; err != nil || !reflect.DeepEqual(endpoint, current) {
		t.Fatal("migration rewrote deletion metadata", err)
	}
	var after model.TrafficRecord
	if err := db.First(&after, row.ID).Error; err != nil || !reflect.DeepEqual(ledger, after) {
		t.Fatal("migration changed ledger or original FK", err)
	}
	target := diagnosticSchemaDB(t, mysql)
	principalRuntimeCopy(t, db, target)
	var copied model.ProtocolEndpoint
	if err := target.Unscoped().First(&copied, endpoint.ID).Error; err != nil || !reflect.DeepEqual(endpoint, copied) {
		t.Fatal("copy lost deletion authority", err)
	}
	after = model.TrafficRecord{}
	if err := target.First(&after, row.ID).Error; err != nil || !reflect.DeepEqual(ledger, after) {
		t.Fatal("copy changed financial history", err)
	}
	// A recorded lost deletion marker is corruption, never a legacy NULL or repairable default.
	if err := db.Migrator().DropIndex("protocol_endpoints", "idx_protocol_endpoints_deletion"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE protocol_endpoints DROP COLUMN deleted_at").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "deletion metadata") {
		t.Fatal("startup fabricated lost deletion authority", err)
	}
	err := datastore.WithCopyDestination(t.Context(), target, func(tx *gorm.DB) error { return datastore.CopyApplicationData(db, tx) })
	if err == nil || !strings.Contains(err.Error(), "deletion metadata") {
		t.Fatal("copy admitted lost deletion authority", err)
	}
	if db.Migrator().HasColumn("protocol_endpoints", "deleted_at") {
		t.Fatal("failed preflight repaired source")
	}
}
