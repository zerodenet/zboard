package datastore_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

func TestSQLiteObservationMaterialMigrationAndRecovery(t *testing.T) {
	db, err := datastore.OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "material-schema.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	assertObservationMaterialMigration(t, db)
}
func TestMySQLObservationMaterialMigrationAndRecovery(t *testing.T) {
	db, _ := hourlyMySQLFixture(t)
	assertObservationMaterialMigration(t, db)
}

func assertObservationMaterialMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("fresh", err)
	}
	const version = "0061_node_observation_material_retention.up.sql"
	path := version
	if datastore.IsSQLite(db) {
		path = "sqlite/0061_node_observation_material_retention.sql"
	}
	raw, err := migrations.Files.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var ddl []string
	for _, s := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(s) != "" {
			ddl = append(ddl, strings.TrimSpace(s))
		}
	}
	if len(ddl) != 6 {
		t.Fatal("formal DDL inventory", len(ddl))
	}
	indexes := []struct{ Table, Name string }{{"node_observation_intents", "idx_observation_material_review"}, {"node_observation_states", "idx_observation_latest_reference"}, {"node_observation_states", "idx_observation_stats_reference"}, {"node_observation_states", "idx_observation_floor_reference"}}
	check := func() {
		t.Helper()
		var n int64
		if err := db.Table("schema_migrations").Where("version = ?", version).Count(&n).Error; err != nil || n != 1 {
			t.Fatal("receipt", n, err)
		}
		for _, i := range indexes {
			if !db.Migrator().HasIndex(i.Table, i.Name) {
				t.Fatal("missing index", i.Name)
			}
		}
	}
	check()
	at := time.Unix(1700000000, 123456789).UTC()
	position := network.EventPosition{AssetTag: strings.Repeat("a", 64), ID: "preserved-schema-source", CoreInstanceID: "runtime", Sequence: 1, OccurredAt: at, ReceivedAt: at}
	source := network.ObservationSource{Observation: network.NodeObservation{NodeID: 91, Latest: position}, Kind: "flow.updated", Payload: []byte{0, 255, 1, 2}}
	var id string
	if err := db.Transaction(func(tx *gorm.DB) error { var e error; id, e = observationstore.Enqueue(tx, source); return e }); err != nil {
		t.Fatal(err)
	}
	var initial model.NodeObservationIntent
	if err := db.First(&initial, "id = ?", id).Error; err != nil || initial.MaterialRetiredAt != nil || !initial.MaterialReviewAt.Equal(initial.CreatedAt.Add(observationstore.MaterialMinimumAge)) {
		t.Fatal("new lifecycle defaults", initial, err)
	}
	preserve := func() {
		t.Helper()
		var h model.NodeObservationIntent
		if err := db.First(&h, "id = ?", id).Error; err != nil || h.MaterialRetiredAt != nil || h.ID != initial.ID || h.MaterialBytes != initial.MaterialBytes || h.PartCount != initial.PartCount {
			t.Fatal("migration changed original receipt", h, err)
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			got, e := observationstore.Enqueue(tx, source)
			if e == nil && got != id {
				return fmt.Errorf("identity changed")
			}
			return e
		}); err != nil {
			t.Fatal("migration damaged retained bytes", err)
		}
	}
	dropIndexes := func() {
		t.Helper()
		for _, i := range indexes {
			if db.Migrator().HasIndex(i.Table, i.Name) {
				if err := db.Migrator().DropIndex(i.Table, i.Name); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	// A genuinely old schema gets compatible defaults without rewriting parts.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		t.Fatal(err)
	}
	dropIndexes()
	for _, c := range []string{"material_review_at", "material_retired_at"} {
		if err := db.Migrator().DropColumn("node_observation_intents", c); err != nil {
			t.Fatal(err)
		}
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("old upgrade", err)
	}
	check()
	preserve()
	// Interrupt after the first ALTER. Resume every remaining formal step.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		t.Fatal(err)
	}
	dropIndexes()
	if err := db.Migrator().DropColumn("node_observation_intents", "material_review_at"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("DDL interruption", err)
	}
	check()
	preserve()
	// The standalone published DDL and the owned recovery path must agree.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		t.Fatal(err)
	}
	dropIndexes()
	for _, c := range []string{"material_review_at", "material_retired_at"} {
		if err := db.Migrator().DropColumn("node_observation_intents", c); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range ddl {
		if err := db.Exec(s).Error; err != nil {
			t.Fatal("formal DDL", err)
		}
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("DDL before receipt", err)
	}
	check()
	preserve()
	// Recorded missing markers are lost evidence, not zero/null migration data.
	for i, c := range []string{"material_retired_at", "material_review_at"} {
		if err := db.Migrator().DropIndex("node_observation_intents", "idx_observation_material_review"); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().DropColumn("node_observation_intents", c); err != nil {
			t.Fatal(err)
		}
		if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "missing") {
			t.Fatal("recorded column loss accepted", c, err)
		}
		if db.Migrator().HasColumn("node_observation_intents", c) {
			t.Fatal("lost evidence recreated", c)
		}
		if err := db.Exec(ddl[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal("known empty fixture restoration", err)
		}
	}
	// Derived missing indexes recover; a different existing definition fails.
	dropIndexes()
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("index recovery", err)
	}
	check()
	preserve()
	query := "SELECT id FROM node_observation_intents WHERE status = ? AND material_retired_at IS NULL AND material_review_at <= ? ORDER BY material_review_at,id LIMIT 1"
	if datastore.IsSQLite(db) {
		var plan []struct{ Detail string }
		if err := db.Raw("EXPLAIN QUERY PLAN "+query, "applied", time.Now().UTC()).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		s := fmt.Sprint(plan)
		if !strings.Contains(s, "idx_observation_material_review") || strings.Contains(s, "TEMP B-TREE") {
			t.Fatal("retention metadata discovery lacks ordered index", s)
		}
	} else {
		var plan []struct {
			Key   string `gorm:"column:key"`
			Extra string
		}
		if err := db.Raw("EXPLAIN "+query, "applied", time.Now().UTC()).Scan(&plan).Error; err != nil || len(plan) != 1 || plan[0].Key != "idx_observation_material_review" || strings.Contains(strings.ToLower(plan[0].Extra), "filesort") {
			t.Fatal("real MySQL retention discovery plan", plan, err)
		}
	}
	if err := db.Migrator().DropIndex("node_observation_intents", "idx_observation_material_review"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE INDEX idx_observation_material_review ON node_observation_intents(status)").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil {
		t.Fatal("wrong retention index accepted")
	}
	preserve()
}
