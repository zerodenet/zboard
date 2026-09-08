package plugins

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/security"
	"gorm.io/gorm"
)

func testManager(t testing.TB, keys map[string]string) (*Manager, *gorm.DB, Options) {
	t.Helper()
	db, err := datastore.OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := db.DB()
	t.Cleanup(func() { sql.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	cipher, err := security.NewCredentialCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Directory: t.TempDir(), TrustedPublishers: keys}
	m, err := NewManager(db, cipher, opts, "v0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m, db, opts
}
func TestOfflineInstallEnableRevokeAndKeepCoreOwnership(t *testing.T) {
	raw, keys := fixturePackage(t, nil)
	m, db, _ := testManager(t, keys)
	v, err := m.Import(raw, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if v.Enabled {
		t.Fatal("import enabled plugin")
	}
	if _, err := m.CreateSession(v.ID, "home", "public", 0, false, false); err == nil {
		t.Fatal("disabled UI was accessible")
	}
	v, err = m.Action(context.Background(), v.ID, "enable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.CreateSession(v.ID, "home", "public", 0, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Asset(s.Token, "ui/index.html"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.CreateSession(v.ID, "settings", "admin", 1, false, true); err == nil {
		t.Fatal("nonadmin config access")
	}
	if _, err := m.CheckSession(s.Token, 2, false); err == nil {
		t.Fatal("cross-user session accepted")
	}
	if _, err := m.Action(context.Background(), v.ID, "users.update", "admin", v.Generation, false, ""); err == nil {
		t.Fatal("undeclared core mutation accepted")
	}
	if _, err := m.SaveConfig(context.Background(), v.ID, "admin", 0, []byte(`{"secret":"do-not-return"}`)); err != nil {
		t.Fatal(err)
	}
	var stored model.PluginInstallation
	db.First(&stored, "id = ?", v.ID)
	if stored.ConfigCiphertext == "" || stored.ConfigCiphertext == `{"secret":"do-not-return"}` {
		t.Fatal("config not encrypted")
	}
	v, err = m.Action(context.Background(), v.ID, "disable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Asset(s.Token, "ui/index.html"); err == nil {
		t.Fatal("old page session survived disable")
	}
	v, err = m.Action(context.Background(), v.ID, "uninstall", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := m.Config(v.ID)
	if err != nil || !c.Configured {
		t.Fatal("uninstall lost config")
	}
	var users int64
	db.Model(&model.User{}).Count(&users)
	if users != 0 {
		t.Fatal("plugin touched core users")
	}
}
func TestRestartRestoresEnabledPackagesAndFencesStandbyHost(t *testing.T) {
	raw, keys := fixturePackage(t, nil)
	m, db, opts := testManager(t, keys)
	v, err := m.Import(raw, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Action(context.Background(), v.ID, "enable", "admin", v.Generation, false, ""); err != nil {
		t.Fatal(err)
	}
	other, err := NewManager(db, m.cipher, opts, "v0.0.1")
	if err != nil {
		t.Fatal("standby console initialization failed", err)
	}
	if _, err := other.CreateSession(v.ID, "home", "public", 0, false, false); !errors.Is(err, ErrUnavailable) {
		t.Fatal("standby host granted execution", err)
	}
	other.Close()
	m.Close()
	next, err := NewManager(db, m.cipher, opts, "v0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	pages, err := next.Pages("public", 0, false)
	if err != nil || len(pages) != 1 {
		t.Fatal(pages, err)
	}
}
func TestLostLeaseAndStaleRevisionDenyMutations(t *testing.T) {
	raw, keys := fixturePackage(t, nil)
	m, db, _ := testManager(t, keys)
	v, err := m.Import(raw, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveConfig(context.Background(), v.ID, "admin", 99, []byte(`{}`)); err != ErrConflict {
		t.Fatal(err)
	}
	db.Model(&model.PluginHostLease{}).Where("id = 1").Update("expires_at", time.Now().UTC().Add(-time.Hour))
	if _, err := m.Action(context.Background(), v.ID, "enable", "admin", v.Generation, false, ""); err != ErrUnavailable {
		t.Fatal(err)
	}
}
func TestDatabaseConfigFailureKeepsCommittedConfiguration(t *testing.T) {
	raw, keys := fixturePackage(t, nil)
	m, db, _ := testManager(t, keys)
	v, err := m.Import(raw, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveConfig(context.Background(), v.ID, "admin", 0, []byte(`{"name":"old"}`)); err != nil {
		t.Fatal(err)
	}
	db.Exec(`CREATE TRIGGER fail_plugin_config BEFORE UPDATE OF config_ciphertext ON plugin_installations BEGIN SELECT RAISE(ABORT, 'injected failure'); END`)
	if _, err := m.SaveConfig(context.Background(), v.ID, "admin", 1, []byte(`{"name":"new"}`)); err == nil {
		t.Fatal("save succeeded despite failed database")
	}
	current, err := m.load(v.ID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := m.config(current)
	if err != nil || string(saved) != `{"name":"old"}` || current.ConfigRevision != 1 {
		t.Fatal(string(saved), err)
	}
}

func TestConfigRequiresDeclaredCapabilityAndArchiveCanBeRepaired(t *testing.T) {
	raw, keys := fixturePackage(t, func(m *Manifest, _ map[string][]byte) {
		m.Capabilities = []string{"zboard.ui.page.v1"}
		m.Contributions.Pages = m.Contributions.Pages[:2]
	})
	m, _, opts := testManager(t, keys)
	v, err := m.Import(raw, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveConfig(context.Background(), v.ID, "admin", 0, []byte(`{}`)); err == nil {
		t.Fatal("undeclared config capability accepted")
	}
	if err := os.WriteFile(filepath.Join(opts.Directory, "versions", v.Digest, "package.zbplugin"), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Import(raw, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.packageFor(v); err != nil {
		t.Fatal("archive not repaired", err)
	}
}

func TestLateConfigurationRequestCannotCrossDisableGeneration(t *testing.T) {
	raw, keys := fixturePackage(t, nil)
	m, _, _ := testManager(t, keys)
	v, err := m.Import(raw, "admin")
	if err != nil {
		t.Fatal(err)
	}
	session, err := m.CreateSession(v.ID, "settings", "admin", 1, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CheckSession(session.Token, 1, true); err != nil {
		t.Fatal(err)
	}
	// Simulate disable after HTTP session preflight but before mutation.
	if _, err := m.Action(context.Background(), v.ID, "disable", "admin", v.Generation, false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveSessionConfig(context.Background(), session.Token, 1, true, "admin", 0, []byte(`{"late":true}`)); err == nil {
		t.Fatal("stale session wrote config")
	}
	view, err := m.Config(v.ID)
	if err != nil || view.Configured {
		t.Fatal(view, err)
	}
}
