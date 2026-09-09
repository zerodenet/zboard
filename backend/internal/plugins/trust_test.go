package plugins

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestOfflineTrustRequiresExactConfirmationAndStaysPluginScoped(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	raw := fixtureSignedPackage(t, priv, pub, nil)
	m, db, opts := testManager(t, nil)
	preview, err := m.PreviewImport(raw, "")
	if err != nil || preview.Trusted {
		t.Fatal(preview, err)
	}
	var count int64
	db.Model(&model.PluginInstallation{}).Count(&count)
	if count != 0 {
		t.Fatal("inspection persisted installation")
	}
	if _, err := m.Import(raw, "admin"); err == nil {
		t.Fatal("self-signed key auto trusted")
	}
	if _, err := m.ImportConfirmed(raw, "admin", "", "wrong", preview.Fingerprint); err == nil {
		t.Fatal("confirmation not bound to package")
	}
	if _, err := m.ImportConfirmed(raw, "admin", "", preview.Digest, "wrong"); err == nil {
		t.Fatal("confirmation not bound to key")
	}
	v, err := m.ImportConfirmed(raw, "admin", "", preview.Digest, preview.Fingerprint)
	if err != nil || v.Enabled || v.SigningKey != base64.StdEncoding.EncodeToString(pub) {
		t.Fatal(v, err)
	}
	if _, err := m.Import(raw, "admin"); err != nil {
		t.Fatal("same key required another confirmation", err)
	}
	other := fixtureSignedPackage(t, priv, pub, func(m *Manifest, _ map[string][]byte) { m.ID = "example.other" })
	if _, err := m.Import(other, "admin"); err == nil {
		t.Fatal("trust leaked across plugins")
	}
	changed, _ := fixturePackage(t, nil)
	if _, err := m.PreviewImport(changed, ""); err == nil {
		t.Fatal("silent key replacement accepted")
	}
	v, err = m.Action(context.Background(), v.ID, "enable", "admin", v.Generation+1, false, "")
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	next, err := NewManager(db, m.cipher, opts, "v0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if _, err := next.packageFor(v); err != nil {
		t.Fatal("persisted key lost on restart", err)
	}
	v, err = next.load(v.ID)
	if err != nil {
		t.Fatal(err)
	}
	v, err = next.Action(context.Background(), v.ID, "uninstall", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := next.Import(changed, "admin"); err == nil {
		t.Fatal("uninstall allowed key takeover")
	}
}

func TestFailedImportDoesNotSavePublisherTrust(t *testing.T) {
	raw, _ := fixturePackage(t, nil)
	m, db, _ := testManager(t, nil)
	preview, err := m.PreviewImport(raw, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_plugin_install BEFORE INSERT ON plugin_installations BEGIN SELECT RAISE(ABORT, 'injected failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := m.ImportConfirmed(raw, "admin", "", preview.Digest, preview.Fingerprint); err == nil {
		t.Fatal("injected failure ignored")
	}
	preview, err = m.PreviewImport(raw, "")
	if err != nil || preview.Trusted {
		t.Fatal("failed install granted trust", preview, err)
	}
	var count int64
	db.Model(&model.PluginVersion{}).Count(&count)
	if count != 0 {
		t.Fatal("partial version persisted")
	}
}
