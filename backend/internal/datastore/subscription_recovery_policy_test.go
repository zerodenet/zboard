package datastore_test

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteRecoveryPolicyInitializationAndLostSource(t *testing.T) {
	db, err := datastore.OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	assertRecoveryPolicyMigration(t, db)
}
func TestMySQLRecoveryPolicyInitializationAndLostSource(t *testing.T) {
	db, _ := hourlyMySQLFixture(t)
	assertRecoveryPolicyMigration(t, db)
}
func assertRecoveryPolicyMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	var policy model.SystemConfig
	if err := db.Where("config_key = ?", "subscription_renewal_grace_days").First(&policy).Error; err != nil || policy.Value != "7" || policy.ValueType != "int" || policy.IsPublic || policy.IsSecret {
		t.Fatal("missing private default recovery policy", policy, err)
	}
	if err := db.Model(&policy).Updates(map[string]any{"value": "14", "revision": uint64(8)}).Error; err != nil {
		t.Fatal(err)
	}
	// A process can stop after the policy write but before the migration record.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", model.SubscriptionRecoveryPolicySchemaVersion).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal(err)
		}
	}
	var retained model.SystemConfig
	if err := db.First(&retained, policy.ID).Error; err != nil || retained.Value != "14" || retained.Revision != 8 {
		t.Fatal("interrupted/repeated initialization changed actual policy", retained, err)
	}
	defaults := platformstore.SystemConfigDefaults{DB: db}
	if err := defaults.ReconcileSystemConfigDefaults(context.Background(), platform.DefaultSystemConfigs()); err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&policy).Error; err != nil {
		t.Fatal(err)
	}
	if err := defaults.ReconcileSystemConfigDefaults(context.Background(), platform.DefaultSystemConfigs()); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "restore before startup") {
		t.Fatal("lost configured window silently became seven days", err)
	}
	var count int64
	if err := db.Model(&model.SystemConfig{}).Where("config_key = ?", "subscription_renewal_grace_days").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("preflight recreated missing source", count, err)
	}
	if err := db.Create(&retained).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&model.SystemConfig{}); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "restore before startup") {
		t.Fatal("lost configuration table was auto-created before source preflight", err)
	}
	if db.Migrator().HasTable(&model.SystemConfig{}) {
		t.Fatal("preflight recreated lost policy table")
	}
}
