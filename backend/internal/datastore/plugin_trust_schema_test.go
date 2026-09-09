package datastore

import (
	"path/filepath"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestSigningTrustUpgradePreservesExistingPluginAndDoesNotGrantLocalTrust(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := db.DB()
	defer sql.Close()
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	row := model.PluginInstallation{ID: "example.old", Name: "Existing", Publisher: "publisher", State: "disabled", Generation: 7, ConfigCiphertext: "retained"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"SigningKey", "LocalTrust"} {
		if err := db.Migrator().DropColumn(&model.PluginInstallation{}, column); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Where("version = ?", "0005_plugin_signing_key.up.sql").Delete(&schemaMigration{}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := RunMigrations(db); err != nil {
			t.Fatal(err)
		}
	}
	var restored model.PluginInstallation
	if err := db.First(&restored, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if restored.Name != row.Name || restored.Generation != 7 || restored.ConfigCiphertext != "retained" || restored.SigningKey != "" || restored.LocalTrust {
		t.Fatal(restored)
	}
}
