package datastore_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

func TestSQLiteNodeAssetOwnedMigration(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "asset-migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	assertNodeAssetOwnedMigration(t, db)
}
func TestMySQLNodeAssetOwnedMigration(t *testing.T) {
	db, _ := hourlyMySQLFixture(t)
	assertNodeAssetOwnedMigration(t, db)
}
func assertNodeAssetOwnedMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	const receipt = "0059_node_asset_identity.up.sql"
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	original := model.Node{Name: "migration-node", Config: `{"preserve":"canonical"}`, NodeCredential: "unchanged-credential"}
	if err := db.Create(&original).Error; err != nil {
		t.Fatal(err)
	}
	tag := original.AssetTag
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	var current model.Node
	if err := db.First(&current, original.ID).Error; err != nil || current.AssetTag != tag {
		t.Fatal("repeat regenerated identity", err)
	}
	// Compare the stored baseline: MySQL JSON normalizes input representation.
	canonical := current
	t.Logf("persisted baseline matches input: name=%t config=%t credential=%t", canonical.Name == original.Name, canonical.Config == original.Config, canonical.NodeCredential == original.NodeCredential)
	// Formal additive asset, then owned future-label initialization. This is not
	// a claim that external execution of ADD alone initializes/records authority.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", receipt).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn("nodes", "asset_tag"); err != nil {
		t.Fatal(err)
	}
	asset := receipt
	if datastore.IsSQLite(db) {
		asset = "sqlite/0059_node_asset_identity.sql"
	}
	raw, err := migrations.Files.ReadFile(asset)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(raw)).Error; err != nil {
		t.Fatal("formal additive column", err)
	}
	sentinel := errors.New("test asset receipt persistence failure")
	const hook = "test:node_asset_receipt"
	if err := db.Callback().Create().Before("gorm:create").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table == "schema_migrations" {
			tx.AddError(sentinel)
		}
	}); err != nil {
		t.Fatal(err)
	}
	err = datastore.ReconcileNodeAssetForTest(db)
	if removeErr := db.Callback().Create().Remove(hook); removeErr != nil {
		t.Fatal(removeErr)
	}
	if !errors.Is(err, sentinel) {
		t.Fatal("receipt failure was hidden", err)
	}
	var initialized model.Node
	if err := db.First(&initialized, original.ID).Error; err != nil || initialized.AssetTag == "" {
		t.Fatal("controlled initialization did not persist before receipt failure", err)
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", receipt).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("failed receipt was recorded", err)
	}
	if err := datastore.ReconcileNodeAssetForTest(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&current, original.ID).Error; err != nil || current.AssetTag != initialized.AssetTag || current.Name != canonical.Name || current.Config != canonical.Config || current.NodeCredential != canonical.NodeCredential {
		t.Fatal("re-entry rekeyed or changed canonical node", err)
	}
	// A recorded complete row explicitly unknown stays unknown, not backfilled.
	if err := db.Table("nodes").Where("id = ?", original.ID).Update("asset_tag", "").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.ReconcileNodeAssetForTest(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&current, original.ID).Error; err != nil || current.AssetTag != "" {
		t.Fatal("receipt replay guessed a missing per-item identity", err)
	}
	if err := db.Migrator().DropColumn("nodes", "asset_tag"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "recorded node asset identity is missing") {
		t.Fatal("recorded structural loss not rejected", err)
	}
	if db.Migrator().HasColumn("nodes", "asset_tag") {
		t.Fatal("AutoMigrate concealed identity loss")
	}
}
