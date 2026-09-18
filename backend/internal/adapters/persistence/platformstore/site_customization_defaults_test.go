package platformstore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestSiteCustomizationDefaultsAreAtomicAndPreserveValues(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "site-defaults.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemConfig{ConfigKey: "legacy_terms", Name: "Legacy", Value: " https://example.test/terms ", ValueType: "string", Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	store := SiteCustomizationDefaults{DB: db}
	definitions := []platform.SiteCustomizationDefault{
		{ConfigKey: "terms", LegacyKey: "legacy_terms", Name: "Terms", Value: "default", ValueType: "string", IsPublic: true, Revision: 1},
		{ConfigKey: "logo", Name: "Logo", Value: "default-logo", ValueType: "string", IsPublic: true, Revision: 1},
	}
	if err := store.ReconcileSiteCustomizationDefaults(context.Background(), definitions); err != nil {
		t.Fatal(err)
	}
	var terms model.SystemConfig
	if err := db.Where("config_key = ?", "terms").First(&terms).Error; err != nil || terms.Value != "https://example.test/terms" {
		t.Fatalf("legacy promotion = %+v err=%v", terms, err)
	}
	if err := db.Model(&terms).Update("value", "operator value").Error; err != nil {
		t.Fatal(err)
	}
	definitions[0].Name = "Updated Terms"
	if err := store.ReconcileSiteCustomizationDefaults(context.Background(), definitions); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("config_key = ?", "terms").First(&terms).Error; err != nil || terms.Value != "operator value" || terms.Name != "Updated Terms" {
		t.Fatalf("operator value preservation = %+v err=%v", terms, err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_site_default BEFORE INSERT ON system_configs WHEN NEW.config_key = 'broken' BEGIN SELECT RAISE(ABORT, 'failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	broken := []platform.SiteCustomizationDefault{{ConfigKey: "first", Name: "First", Value: "1", ValueType: "string"}, {ConfigKey: "broken", Name: "Broken", Value: "2", ValueType: "string"}}
	if err := store.ReconcileSiteCustomizationDefaults(context.Background(), broken); err == nil {
		t.Fatal("trigger failure accepted")
	}
	var count int64
	if err := db.Model(&model.SystemConfig{}).Where("config_key = ?", "first").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("partial defaults persisted count=%d err=%v", count, err)
	}
}
