package platformstore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestSystemConfigDefaultsAreAtomicAndPreserveOperatorValues(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "system-defaults.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemConfig{ConfigKey: "existing", Name: "Existing", Value: "operator", ValueType: "string"}).Error; err != nil {
		t.Fatal(err)
	}
	store := SystemConfigDefaults{DB: db}
	definitions := []platform.SystemConfigDefault{{Key: "existing", Name: "New", Value: "default", ValueType: "string"}, {Key: "created", Name: "Created", Value: "value", ValueType: "string"}}
	if err := store.ReconcileSystemConfigDefaults(context.Background(), definitions); err != nil {
		t.Fatal(err)
	}
	var existing model.SystemConfig
	if err := db.Where("config_key = ?", "existing").First(&existing).Error; err != nil || existing.Value != "operator" || existing.Name != "Existing" {
		t.Fatalf("existing default overwritten: %+v err=%v", existing, err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_system_default BEFORE INSERT ON system_configs WHEN NEW.config_key = 'broken' BEGIN SELECT RAISE(ABORT, 'failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	broken := []platform.SystemConfigDefault{{Key: "first", Name: "First", Value: "1", ValueType: "string"}, {Key: "broken", Name: "Broken", Value: "2", ValueType: "string"}}
	if err := store.ReconcileSystemConfigDefaults(context.Background(), broken); err == nil {
		t.Fatal("trigger failure accepted")
	}
	var count int64
	if err := db.Model(&model.SystemConfig{}).Where("config_key = ?", "first").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("partial defaults persisted count=%d err=%v", count, err)
	}
}
