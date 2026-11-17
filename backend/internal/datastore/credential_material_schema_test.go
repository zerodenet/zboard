package datastore

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestSubscriptionMaterialSchemaDefaultsAndLostAuthority(t *testing.T) {
	db, e := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "material.db"))
	if e != nil {
		t.Fatal(e)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	if e := RunMigrations(db); e != nil {
		t.Fatal(e)
	}
	for key, want := range map[string]string{"subscription_credential_material_enabled": "false", "subscription_credential_compatibility_days": "30"} {
		var r model.SystemConfig
		if e := db.Where("config_key=?", key).First(&r).Error; e != nil || r.Value != want {
			t.Fatal(key, r.Value, e)
		}
	}
	if e := db.Model(&model.SystemConfig{}).Where("config_key=?", "subscription_credential_compatibility_days").Update("value", "45").Error; e != nil {
		t.Fatal(e)
	}
	if e := RunMigrations(db); e != nil {
		t.Fatal("repeat migration", e)
	}
	var r model.SystemConfig
	if e := db.Where("config_key=?", "subscription_credential_compatibility_days").First(&r).Error; e != nil || r.Value != "45" {
		t.Fatal("overwrote admin setting", e)
	}
	if e := db.Migrator().DropColumn("protocol_credentials", "material_verifier"); e != nil {
		t.Fatal(e)
	}
	if e := RunMigrations(db); e == nil || !strings.Contains(e.Error(), "recorded credential attribution missing") {
		t.Fatal("lost verifier silently recreated", e)
	}
	if db.Migrator().HasColumn("protocol_credentials", "material_verifier") {
		t.Fatal("lost verifier recreated")
	}
}
