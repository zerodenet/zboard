package datastore_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestSQLiteNodeObservationOwnedMigration(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "handoff.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	assertObservationMigration(t, db)
}
func TestMySQLNodeObservationOwnedMigration(t *testing.T) {
	db, _ := hourlyMySQLFixture(t)
	assertObservationMigration(t, db)
}
func assertObservationMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	if err := datastore.ReconcileZeroEventSchema(db); err != nil {
		t.Fatal(err)
	}
	const version = "0060_node_observation_handoff.up.sql"
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		t.Fatal(err)
	}
	when := time.Unix(1700000000, 0).UTC()
	if err := db.Table("zero_event_node_cursors").Create(map[string]any{"node_id": 981, "core_instance_id": "legacy-floor", "sequence": 100, "config_revision": 2, "occurred_at": when, "updated_at": when}).Error; err != nil {
		t.Fatal(err)
	}
	injected := errors.New("receipt persistence failed")
	const hook = "test:observation_receipt"
	if err := db.Callback().Create().Before("gorm:create").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table == "schema_migrations" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	err := datastore.ReconcileNodeObservationForTest(db)
	_ = db.Callback().Create().Remove(hook)
	if !errors.Is(err, injected) {
		t.Fatalf("receipt failure: %v", err)
	}
	var n int64
	if err := db.Table("schema_migrations").Where("version = ?", version).Count(&n).Error; err != nil || n != 0 {
		t.Fatal("failed migration recorded receipt", err)
	}
	var floor model.NodeObservationState
	if err := db.First(&floor, "node_id = ? AND asset_tag = ?", 981, "").Error; err != nil || floor.FloorReference == "" || floor.CurrentKnown || floor.StatsReference != "" || floor.LatestReference != "" {
		t.Fatal("legacy floor fabricated Stats/Latest identity", err)
	}
	original := floor.FloorReference
	if err := datastore.ReconcileNodeObservationForTest(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&floor, "node_id = ? AND asset_tag = ?", 981, "").Error; err != nil || floor.FloorReference != original {
		t.Fatal("reentry changed legacy floor", err)
	}
	if datastore.IsSQLite(db) {
		for _, definition := range []string{
			"CREATE UNIQUE INDEX idx_node_observation_ready ON node_observation_intents(status,available_at,id)",
			"CREATE INDEX idx_node_observation_ready ON node_observation_intents(status,available_at,id) WHERE status = 'held'",
			"CREATE INDEX idx_node_observation_ready ON node_observation_intents(status DESC,available_at,id)",
			"CREATE INDEX idx_node_observation_ready ON node_observation_intents(status COLLATE NOCASE,available_at,id)",
			"CREATE INDEX idx_node_observation_ready ON node_observation_intents(lower(status),available_at,id)",
		} {
			if err := db.Exec("DROP INDEX idx_node_observation_ready").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(definition).Error; err != nil {
				t.Fatal(err)
			}
			if err := datastore.ReconcileNodeObservationForTest(db); err == nil {
				t.Fatal("unsafe ready-index shape accepted", definition)
			}
			if err := db.Exec("DROP INDEX idx_node_observation_ready").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("CREATE INDEX idx_node_observation_ready ON node_observation_intents(status,available_at,id)").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.First(&floor, "node_id = ? AND asset_tag = ?", 981, "").Error; err != nil || floor.FloorReference != original {
				t.Fatal("shape rejection changed source evidence", err)
			}
		}
	}
	// Missing recorded identity/evidence cannot be recreated empty, including
	// before SQLite AutoMigrate. A lost non-key source column is also rejected.
	if err := db.Migrator().DropColumn("node_observation_parts", "data"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil {
		t.Fatal("startup silently recreated lost source material")
	}
}
