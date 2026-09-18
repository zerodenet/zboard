package networkstore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestSSHHostTrustEnrollsAndRejectsChangedKeys(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "ssh-trust.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	node := model.Node{Name: "ssh", Config: "{}"}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	service := network.SSHHostTrust{Repository: SSHHostTrust{DB: db}}
	first := "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	second := "SHA256:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	if err := service.Pin(context.Background(), node.ID, "", first); err != nil {
		t.Fatal(err)
	}
	if err := service.Pin(context.Background(), node.ID, first, first); err != nil {
		t.Fatal(err)
	}
	if err := service.Pin(context.Background(), node.ID, first, second); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("changed host key error = %v", err)
	}
	if err := service.Pin(context.Background(), node.ID, "", second); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("concurrent enrollment error = %v", err)
	}
}
