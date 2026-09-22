package plugins

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
)

func TestSignedPluginsSeparateConfigurationAndStorageReadsFromWrites(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manager, _, _ := testManager(t, map[string]string{"test.publisher": base64.StdEncoding.EncodeToString(public)})
	makePackage := func(id string, config, storage string) []byte {
		return fixtureSignedPackage(t, private, public, func(manifest *Manifest, _ map[string][]byte) {
			manifest.ID = id
			manifest.Capabilities = []string{PageCapability, config, storage}
			manifest.Data = &DataManifest{Version: 1, MinCompatibleVersion: 1, Migrations: []DataMigration{{Version: 1}}}
		})
	}
	readOnly, err := manager.Import(makePackage("example.readonly", ConfigReadCapability, StorageReadCapability), "admin")
	if err != nil {
		t.Fatal(err)
	}
	writeOnly, err := manager.Import(makePackage("example.writeonly", ConfigWriteCapability, StorageWriteCapability), "admin")
	if err != nil {
		t.Fatal(err)
	}
	readSession, err := manager.CreateSession(readOnly.ID, "settings", "admin", 1, true, true)
	if err != nil {
		t.Fatal(err)
	}
	writeSession, err := manager.CreateSession(writeOnly.ID, "settings", "admin", 1, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Config(readOnly.ID); err != nil {
		t.Fatalf("read-only configuration load: %v", err)
	}
	if _, err := manager.SaveSessionConfig(context.Background(), readSession.Token, 1, true, "admin", 0, []byte(`{"secret":"blocked"}`)); !errors.Is(err, ErrPermission) {
		t.Fatalf("read-only plugin saved configuration or returned wrong denial: %v", err)
	}
	if _, err := manager.SessionStorage(readSession.Token, 1, true, StorageRequest{Type: "storage.get", Key: "private"}); err != nil {
		t.Fatalf("read-only storage get: %v", err)
	}
	if _, err := manager.SessionStorage(readSession.Token, 1, true, StorageRequest{Type: "storage.put", Key: "private", Value: json.RawMessage(`"blocked"`)}); !errors.Is(err, ErrPermission) {
		t.Fatalf("read-only plugin wrote storage: %v", err)
	}
	if _, err := manager.Config(writeOnly.ID); !errors.Is(err, ErrPermission) {
		t.Fatalf("write-only plugin read configuration: %v", err)
	}
	if view, err := manager.ConfigRevision(writeOnly.ID); err != nil || view.Revision != 0 || len(view.Config) != 0 {
		t.Fatalf("write-only configuration revision: %+v %v", view, err)
	}
	if view, err := manager.SaveSessionConfig(context.Background(), writeSession.Token, 1, true, "admin", 0, []byte(`{"secret":"stored"}`)); err != nil || view.Revision != 1 {
		t.Fatalf("write-only configuration save: %+v %v", view, err)
	}
	if view, err := manager.ConfigRevision(writeOnly.ID); err != nil || view.Revision != 1 || !view.Configured || len(view.Config) != 0 {
		t.Fatalf("write-only configuration metadata leaked or stale: %+v %v", view, err)
	}
	if _, err := manager.SessionStorage(writeSession.Token, 1, true, StorageRequest{Type: "storage.get", Key: "private"}); !errors.Is(err, ErrPermission) {
		t.Fatalf("write-only plugin read storage: %v", err)
	}
	before, err := manager.SessionStorage(writeSession.Token, 1, true, StorageRequest{Type: "storage.head", Key: "private"})
	if err != nil || before.Found || len(before.Value) != 0 {
		t.Fatalf("write-only storage preflight: %+v %v", before, err)
	}
	if _, err := manager.SessionStorage(writeSession.Token, 1, true, StorageRequest{Type: "storage.put", Key: "private", Revision: before.Revision, Value: json.RawMessage(`"stored"`)}); err != nil {
		t.Fatalf("write-only storage put: %v", err)
	}
	if head, err := manager.SessionStorage(writeSession.Token, 1, true, StorageRequest{Type: "storage.head", Key: "private"}); err != nil || head.Revision != before.Revision+1 || !head.Found || len(head.Value) != 0 {
		t.Fatalf("write-only storage head leaked or stale: %+v %v", head, err)
	}
}
