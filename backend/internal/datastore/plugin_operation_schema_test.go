package datastore

import (
	"path/filepath"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestPluginOperationAuditSchemaUpgradeRetainsOldOperations(t *testing.T) {
	db, err := OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := db.DB()
	defer sql.Close()
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	row := model.PluginOperation{ID: "historic", PluginID: "example.plugin", Action: "uninstall", State: "succeeded", Actor: "operator", Message: "retained"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"PreviousPublisher", "Publisher", "SigningKeyFingerprint", "PackageDigest"} {
		if err := db.Migrator().DropColumn(&model.PluginOperation{}, column); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := RunMigrations(db); err != nil {
			t.Fatal(err)
		}
	}
	var restored model.PluginOperation
	if err := db.First(&restored, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if restored.Action != row.Action || restored.Actor != row.Actor || restored.Message != row.Message || restored.Publisher != "" {
		t.Fatal("audit history changed", restored)
	}
	if err := db.Model(&restored).Updates(map[string]any{"previous_publisher": "local.publisher", "publisher": "formal.publisher", "signing_key_fingerprint": "fingerprint", "package_digest": "digest"}).Error; err != nil {
		t.Fatal(err)
	}
}
