package plugins

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestUpgradeRetainsOnlyCurrentPackageAndDisablesRollback(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]string{"test.publisher": base64.StdEncoding.EncodeToString(pub)}
	m, db, opts := testManager(t, keys)
	old, err := m.Import(fixtureSignedPackage(t, priv, pub, nil), "admin")
	if err != nil {
		t.Fatal(err)
	}
	next, err := m.Import(fixtureSignedPackage(t, priv, pub, func(manifest *Manifest, _ map[string][]byte) {
		manifest.Version = "1.1.0"
	}), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if next.Digest == old.Digest || len(next.Versions) != 1 || next.Versions[0].ID != next.VersionID {
		t.Fatal("upgrade retained version history", next.Versions)
	}
	var versions int64
	if err := db.Model(&model.PluginVersion{}).Where("plugin_id = ?", next.ID).Count(&versions).Error; err != nil || versions != 1 {
		t.Fatal("upgrade retained old version metadata", versions, err)
	}
	if _, err := os.Stat(filepath.Join(opts.Directory, "versions", old.Digest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("upgrade retained old package", err)
	}
	if _, err := m.packageFor(next); err != nil {
		t.Fatal("current package cannot be verified", err)
	}
	if _, err := m.Action(context.Background(), next.ID, "rollback", "admin", next.Generation, false, old.VersionID); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatal("rollback operation remained available", err)
	}
	var operations int64
	if err := db.Model(&model.PluginOperation{}).Where("plugin_id = ? AND action = ?", next.ID, "rollback").Count(&operations).Error; err != nil || operations != 0 {
		t.Fatal("rejected rollback created an operation", operations, err)
	}
	next, err = m.Action(context.Background(), next.ID, "uninstall", "admin", next.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(opts.Directory, "versions", next.Digest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("uninstall retained package", err)
	}
	if len(next.Versions) != 1 {
		t.Fatal("uninstall lost current metadata needed for data purge", next.Versions)
	}
}

func TestStartupRemovesLegacyPackageHistoryAndInterruptedWrites(t *testing.T) {
	raw, keys := fixturePackage(t, nil)
	m, db, opts := testManager(t, keys)
	current, err := m.Import(raw, "admin")
	if err != nil {
		t.Fatal(err)
	}
	legacyDigest := strings.Repeat("a", 64)
	if legacyDigest == current.Digest {
		legacyDigest = strings.Repeat("b", 64)
	}
	if err := db.Create(&model.PluginVersion{ID: legacyDigest, PluginID: current.ID, Version: "0.9.0", Digest: legacyDigest, Publisher: current.Publisher, Manifest: manifestJSON(current.Manifest)}).Error; err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(opts.Directory, "versions")
	for _, path := range []string{filepath.Join(root, legacyDigest), filepath.Join(root, ".import-interrupted")} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	repair := filepath.Join(root, current.Digest, ".repair-interrupted")
	if err := os.WriteFile(repair, []byte("incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	m.Close()
	restarted, err := NewManager(db, m.cipher, opts, "v0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	for _, path := range []string{filepath.Join(root, legacyDigest), filepath.Join(root, ".import-interrupted"), repair} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("legacy package artifact survived recovery", path, err)
		}
	}
	if err := db.First(&model.PluginVersion{}, "id = ?", legacyDigest).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("legacy version metadata survived recovery", err)
	}
	if _, err := restarted.packageFor(current); err != nil {
		t.Fatal("recovery removed current package", err)
	}
}
