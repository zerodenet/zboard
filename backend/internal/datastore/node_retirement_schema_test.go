package datastore_test

import (
	"path/filepath"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestSQLiteNodeRetirementSchemaUpgradeRetainsTaskProgress(t *testing.T) {
	db, err := datastore.OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "retirement.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	assertRetirementSchemaUpgrade(t, db)
}

func TestMySQLNodeRetirementSchemaUpgradeRetainsTaskProgress(t *testing.T) {
	db, _ := hourlyMySQLFixture(t)
	assertRetirementSchemaUpgrade(t, db)
}

func assertRetirementSchemaUpgrade(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasTable(&model.NodeRetirement{}) {
		t.Fatal("fresh migration did not create node retirement history")
	}
	// Model an older development database whose baseline has already applied.
	if err := db.Migrator().DropTable(&model.NodeRetirement{}); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	// No node exists: task history must remain writable after node removal.
	row := model.NodeRetirement{NodeID: 4100, NodeName: "retired-node", ActorID: 1, Mode: "retire", State: "failed", RunID: "test-run", Attempt: 2, Steps: `[{"kind":"dns","id":1,"state":"succeeded"}]`, LastError: "retry certificate cleanup"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal(err)
		}
	}
	var restored model.NodeRetirement
	if err := db.First(&restored, "node_id = ?", row.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	if restored.NodeName != row.NodeName || restored.ActorID != row.ActorID || restored.Mode != row.Mode || restored.State != row.State || restored.RunID != row.RunID || restored.Attempt != row.Attempt || restored.Steps != row.Steps || restored.LastError != row.LastError {
		t.Fatalf("repeated migration changed retirement progress: %+v", restored)
	}
}
