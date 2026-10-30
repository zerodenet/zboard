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

const archiveVersion = "0073_node_archive.up.sql"

func TestSQLiteNodeArchiveUpgradeRecoveryAndCopy(t *testing.T) { nodeArchiveUpgrade(t, false) }
func TestMySQLNodeArchiveUpgradeRecoveryAndCopy(t *testing.T)  { nodeArchiveUpgrade(t, true) }
func nodeArchiveUpgrade(t *testing.T, mysql bool) {
	db := diagnosticSchemaDB(t, mysql)
	row := diagnosticSchemaSource(t, db, false)
	var ledger model.TrafficRecord
	if err := db.First(&ledger, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	// Simulate actual pre-archive schema; startup must add no historical state.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", archiveVersion).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropIndex("nodes", "idx_nodes_archive"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE nodes DROP COLUMN archived_at").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("legacy upgrade", err)
	}
	var node model.Node
	if err := db.First(&node, row.NodeID).Error; err != nil || node.ArchivedAt != nil {
		t.Fatal("upgrade inferred retirement", node, err)
	}
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if err := db.Model(&node).Updates(map[string]any{"lifecycle_status": "archived", "archived_at": at, "is_enabled": false, "node_credential_revoked_at": at, "traffic_secret_revoked_at": at}).Error; err != nil {
		t.Fatal(err)
	}
	node = model.Node{}
	if err := db.First(&node, row.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	// Interrupted DDL can resume without rewriting a single archived timestamp.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", archiveVersion).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal("interrupted migration", err)
		}
	}
	var current model.Node
	if err := db.First(&current, node.ID).Error; err != nil || !reflect.DeepEqual(node, current) {
		t.Fatal("migration rewrote archive", err)
	}
	var after model.TrafficRecord
	if err := db.First(&after, row.ID).Error; err != nil || !reflect.DeepEqual(ledger, after) {
		t.Fatal("migration changed ledger or original FK", err)
	}
	target := diagnosticSchemaDB(t, mysql)
	principalRuntimeCopy(t, db, target)
	var copied model.Node
	if err := target.First(&copied, node.ID).Error; err != nil || !reflect.DeepEqual(node, copied) {
		t.Fatal("copy lost archive authority", err)
	}
	after = model.TrafficRecord{}
	if err := target.First(&after, row.ID).Error; err != nil || !reflect.DeepEqual(ledger, after) {
		t.Fatal("copy changed financial history", err)
	}
	// A recorded lost archive is corruption, never a legacy NULL or repairable default.
	if err := db.Migrator().DropIndex("nodes", "idx_nodes_archive"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE nodes DROP COLUMN archived_at").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "archive metadata") {
		t.Fatal("startup fabricated lost archive authority", err)
	}
	err := datastore.WithCopyDestination(t.Context(), target, func(tx *gorm.DB) error { return datastore.CopyApplicationData(db, tx) })
	if err == nil || !strings.Contains(err.Error(), "archive metadata") {
		t.Fatal("copy admitted lost archive authority", err)
	}
	if db.Migrator().HasColumn("nodes", "archived_at") {
		t.Fatal("failed preflight repaired source")
	}
}
