package datastore

import (
	"path/filepath"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestListenAddressMigrationPreservesLegacyAndConfiguredEndpoints(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "listen.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	node := model.Node{Name: "legacy", Address: "192.0.2.1", Config: "{}"}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, Name: "legacy", RuntimeKey: "legacy", Protocol: "vless", Address: "public.example", Port: 443, PublicPort: 8443}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	// Reproduce the old table shape before the append-only migration.
	if err := db.Migrator().DropColumn(&model.ProtocolEndpoint{}, "listen_address"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", "0030_protocol_endpoint_listen_address.up.sql").Error; err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	var got model.ProtocolEndpoint
	if err := db.First(&got, endpoint.ID).Error; err != nil || got.ListenAddress != "0.0.0.0" || got.Address != endpoint.Address || got.Port != 443 || got.PublicPort != 8443 {
		t.Fatalf("legacy endpoint changed during migration: %+v %v", got, err)
	}
	if err := db.Model(&got).Update("listen_address", "::").Error; err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&got, endpoint.ID).Error; err != nil || got.ListenAddress != "::" {
		t.Fatalf("repeat migration reset configured listener: %+v %v", got, err)
	}
}
