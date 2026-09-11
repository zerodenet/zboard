package plugins

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/model"
	"strings"
	"testing"
)

func TestHostAdmitsDeclaredCapabilitiesAndFencesLifecycleSessions(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	keys := map[string]string{"test.publisher": base64.StdEncoding.EncodeToString(pub)}
	raw := fixtureSignedPackage(t, priv, pub, nil)
	m, _, _ := testManager(t, keys)
	v, err := m.Import(raw, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !v.Admission.Accepted || !hasCapability(v, ConfigCapability) {
		t.Fatal("host did not complete admission")
	}
	if hasCapability(v, StorageCapability) {
		t.Fatal("undeclared storage capability admitted")
	}
	session, err := m.CreateSession(v.ID, "settings", "admin", 1, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.SessionStorage(session.Token, 1, true, StorageRequest{Type: "storage.get", Key: "secret"}); err == nil {
		t.Fatal("undeclared storage reachable")
	}
	if _, err = m.Action(context.Background(), v.ID, "migrate", "admin", v.Generation, false, ""); err == nil {
		t.Fatal("manual migration operation still exposed")
	}
	v, err = m.Action(context.Background(), v.ID, "enable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.CheckSession(session.Token, 1, true); err == nil {
		t.Fatal("old session survived enable")
	}
	raw = fixtureSignedPackage(t, priv, pub, func(manifest *Manifest, _ map[string][]byte) {
		manifest.Capabilities = append(manifest.Capabilities, "zboard.core.sql.v1")
	})
	if _, err = m.Import(raw, "admin"); err == nil {
		t.Fatal("host accepted unsupported capability")
	}
	session, err = m.CreateSession(v.ID, "settings", "admin", 1, true, true)
	if err != nil {
		t.Fatal(err)
	}
	replacement := fixtureSignedPackage(t, priv, pub, func(manifest *Manifest, _ map[string][]byte) {
		manifest.Version = "1.1.0"
		manifest.Capabilities = []string{PageCapability}
		pages := []Page{}
		for _, page := range manifest.Contributions.Pages {
			if page.Purpose != "configuration" {
				pages = append(pages, page)
			}
		}
		manifest.Contributions.Pages = pages
	})
	v, err = m.Import(replacement, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if hasCapability(v, ConfigCapability) || !hasCapability(v, PageCapability) {
		t.Fatal("host failed to replace capability scope")
	}
	if _, err = m.CheckSession(session.Token, 1, true); err == nil {
		t.Fatal("removed capability retained its page session")
	}
	if _, err = m.SaveConfig(context.Background(), v.ID, "admin", v.ConfigRevision, []byte(`{}`)); err == nil {
		t.Fatal("removed capability remained callable")
	}

}
func dataPackage(t testing.TB, priv ed25519.PrivateKey, pub ed25519.PublicKey, id string, version int, second []DataChange) []byte {
	return fixtureSignedPackage(t, priv, pub, func(m *Manifest, _ map[string][]byte) {
		m.ID = id
		m.Version = map[int]string{1: "1.0.0", 2: "2.0.0"}[version]
		m.Capabilities = append(m.Capabilities, StorageCapability)
		m.Data = &DataManifest{Version: uint64(version), MinCompatibleVersion: uint64(version), Migrations: []DataMigration{{Version: 1, Changes: []DataChange{{Target: "storage", Operation: "set_default", Key: "legacy", Value: json.RawMessage(`"private"`)}}}}}
		if version == 2 {
			m.Data.Migrations = append(m.Data.Migrations, DataMigration{Version: 2, Changes: second})
		}
	})
}
func TestPluginPrivateDataIsolationCASAndDisableUninstallRetention(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	keys := map[string]string{"test.publisher": base64.StdEncoding.EncodeToString(pub)}
	m, db, _ := testManager(t, keys)
	a, err := importFixture(t, m, dataPackage(t, priv, pub, "example.first", 1, nil))
	if err != nil {
		t.Fatal(err)
	}
	if a.Data.Version != 1 || a.Data.MigrationRequired {
		t.Fatal("install did not initialize data")
	}
	b, err := importFixture(t, m, dataPackage(t, priv, pub, "example.second", 1, nil))
	if err != nil {
		t.Fatal(err)
	}
	sa, err := m.CreateSession(a.ID, "settings", "admin", 1, true, true)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := m.CreateSession(b.ID, "settings", "admin", 1, true, true)
	if err != nil {
		t.Fatal(err)
	}
	first, err := m.SessionStorage(sa.Token, 1, true, StorageRequest{Type: "storage.get", Key: "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	update := StorageRequest{Type: "storage.put", Key: "secret", Revision: first.Revision, Value: json.RawMessage(`"only-first"`)}
	if _, err = m.SessionStorage(sa.Token, 1, false, update); err == nil {
		t.Fatal("nonadmin storage write")
	}
	if _, err = m.SessionStorage(sa.Token, 2, true, update); err == nil {
		t.Fatal("cross-user session write")
	}
	if _, err = m.SessionStorage(sa.Token, 1, true, update); err != nil {
		t.Fatal(err)
	}
	if _, err = m.SessionStorage(sa.Token, 1, true, update); !errors.Is(err, ErrConflict) {
		t.Fatal("stale data revision", err)
	}
	other, err := m.SessionStorage(sb.Token, 1, true, StorageRequest{Type: "storage.get", Key: "secret"})
	if err != nil || other.Found {
		t.Fatal("cross-plugin data leak", err)
	}
	row, _ := m.readData(a.ID)
	if strings.Contains(row.Ciphertext, "only-first") {
		t.Fatal("plaintext storage")
	}
	user := model.User{AccountName: "core", Email: "core@example.com", Password: "external", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	binding := model.ExternalIdentity{ID: "core-binding", UserID: user.ID, PluginID: a.ID, Publisher: "publisher", Issuer: "issuer", Subject: "subject"}
	if err := db.Create(&binding).Error; err != nil {
		t.Fatal(err)
	}
	a, err = m.Action(context.Background(), a.ID, "disable", "admin", a.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	row, _ = m.readData(a.ID)
	if row.Ciphertext == "" || row.Version != 1 {
		t.Fatal("disable removed private data")
	}
	if db.First(&model.ExternalIdentity{}, "id = ?", binding.ID).Error != nil {
		t.Fatal("disable removed core identity")
	}
	a, err = m.Action(context.Background(), a.ID, "uninstall", "admin", a.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	row, _ = m.readData(a.ID)
	if row.Ciphertext == "" || row.Version != 1 {
		t.Fatal("uninstall removed private data")
	}
	if _, err = m.SessionStorage(sa.Token, 1, true, update); err == nil {
		t.Fatal("uninstalled session write")
	}
	a, err = m.Action(context.Background(), a.ID, "purge_data", "admin", a.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	row, _ = m.readData(a.ID)
	if row.Ciphertext != "" || row.Version != 0 || row.Epoch != 1 {
		t.Fatal("purge failed")
	}
	if db.First(&model.User{}, user.ID).Error != nil || db.First(&model.ExternalIdentity{}, "id = ?", binding.ID).Error != nil {
		t.Fatal("purge touched core identity")
	}
	migrations, _ := m.Migrations(a.ID)
	if len(migrations) != 1 {
		t.Fatal("purge removed audit history")
	}
	other, err = m.SessionStorage(sb.Token, 1, true, StorageRequest{Type: "storage.get", Key: "legacy"})
	if err != nil || !other.Found {
		t.Fatal("purge affected another plugin", err)
	}
	reinstalled, err := m.Import(dataPackage(t, priv, pub, a.ID, 1, nil), "admin")
	if err != nil || reinstalled.Data.Version != 1 || reinstalled.Data.Epoch != 1 || reinstalled.Data.MigrationRequired {
		t.Fatal("reinstall did not initialize cleared data automatically", err)
	}

}
func TestPluginUpgradeCommitsRuntimeDataAndVersionTogether(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	keys := map[string]string{"test.publisher": base64.StdEncoding.EncodeToString(pub)}
	m, db, _ := testManager(t, keys)
	ctx := context.Background()
	old, err := m.Import(dataPackage(t, priv, pub, "example.data", 1, nil), "admin")
	if err != nil {
		t.Fatal(err)
	}
	old, err = m.Action(ctx, old.ID, "enable", "admin", old.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	session, err := m.CreateSession(old.ID, "home", "public", 0, false, false)
	if err != nil {
		t.Fatal(err)
	}
	nextRaw := dataPackage(t, priv, pub, old.ID, 2, []DataChange{{Target: "storage", Operation: "rename", Key: "legacy", To: "current"}, {Target: "config", Operation: "set_default", Key: "new_option", Value: json.RawMessage(`true`)}})
	before, _ := m.readData(old.ID)
	if err := db.Exec(`CREATE TRIGGER fail_migration BEFORE INSERT ON plugin_migrations BEGIN SELECT RAISE(ABORT, 'injected failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = m.Import(nextRaw, "admin"); err == nil {
		t.Fatal("upgrade ignored migration failure")
	}
	after, _ := m.readData(old.ID)
	current, _ := m.load(old.ID)
	if after.Ciphertext != before.Ciphertext || after.Version != before.Version || current.Digest != old.Digest || current.Generation != old.Generation || !current.Enabled || current.ConfigRevision != 0 || !current.Admission.Accepted {
		t.Fatal("failed upgrade changed committed state")
	}
	if _, err := m.CheckSession(session.Token, 0, false); err != nil {
		t.Fatal("failed upgrade revoked working session", err)
	}
	if len(current.Versions) != 1 {
		t.Fatal("failed candidate was published")
	}
	records, _ := m.Migrations(old.ID)
	if len(records) != 1 {
		t.Fatal("failed migration left ledger")
	}
	if err := db.Exec(`DROP TRIGGER fail_migration`).Error; err != nil {
		t.Fatal(err)
	}
	next, err := m.Import(nextRaw, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if next.Data.Version != 2 || next.ConfigRevision != 1 || !next.Enabled || next.State != "active" || !next.Admission.Accepted {
		t.Fatal("upgrade did not commit complete lifecycle")
	}
	if _, err = m.CheckSession(session.Token, 0, false); err == nil {
		t.Fatal("old session survived upgrade")
	}
	next, err = m.Action(ctx, next.ID, "disable", "admin", next.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Action(ctx, next.ID, "rollback", "admin", next.Generation, false, old.VersionID); err == nil {
		t.Fatal("incompatible old program restored")
	}
	tampered := fixtureSignedPackage(t, priv, pub, func(manifest *Manifest, _ map[string][]byte) {
		pack, e := ReadPackage(nextRaw, keys)
		if e != nil {
			t.Fatal(e)
		}
		*manifest = pack.Manifest
		manifest.Data.Migrations[0].Changes[0].Value = json.RawMessage(`"changed"`)
	})
	if _, err = m.Import(tampered, "admin"); err == nil {
		t.Fatal("modified migration history accepted during import")
	}
	current, _ = m.load(old.ID)
	if current.Digest != next.Digest {
		t.Fatal("rejected package replaced current version")
	}
}
func TestFailedFirstInstallLeavesNoInstallationOrData(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	keys := map[string]string{"test.publisher": base64.StdEncoding.EncodeToString(pub)}
	m, db, _ := testManager(t, keys)
	if err := db.Exec(`CREATE TRIGGER fail_install BEFORE INSERT ON plugin_installations BEGIN SELECT RAISE(ABORT, 'injected failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := m.Import(dataPackage(t, priv, pub, "example.failed", 1, nil), "admin"); err == nil {
		t.Fatal("install ignored commit failure")
	}
	for _, table := range []string{"plugin_installations", "plugin_versions", "plugin_data", "plugin_migrations", "plugin_authorizations"} {
		var n int64
		if err := db.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatal("partial installation", table, n, err)
		}
	}
	var ops []model.PluginOperation
	db.Find(&ops)
	if len(ops) != 1 || ops[0].State != "failed" {
		t.Fatal("failed install not recorded")
	}
}
func TestPluginStorageQuotasAndSQLMigrationRejected(t *testing.T) {
	obj := map[string]json.RawMessage{"value": json.RawMessage(`"` + strings.Repeat("x", MaxStorageValueBytes) + `"`)}
	if _, err := encodeStorage(obj); err == nil {
		t.Fatal("large value accepted")
	}
	raw, keys := fixturePackage(t, func(m *Manifest, _ map[string][]byte) {
		m.Data = &DataManifest{Version: 1, MinCompatibleVersion: 1, Migrations: []DataMigration{{Version: 1, Changes: []DataChange{{Target: "config", Operation: "sql", Key: "users"}}}}}
	})
	if _, err := ReadPackage(raw, keys); err == nil {
		t.Fatal("SQL migration accepted")
	}
}

func TestPluginRecoveryOwnsAdmissionForExistingInstallations(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	keys := map[string]string{"test.publisher": base64.StdEncoding.EncodeToString(pub)}
	raw := dataPackage(t, priv, pub, "example.recovery", 1, nil)
	m, db, _ := testManager(t, keys)
	v, err := m.Import(raw, "admin")
	if err != nil {
		t.Fatal(err)
	}
	v, err = m.Action(context.Background(), v.ID, "enable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Where("plugin_id = ?", v.ID).Delete(&model.PluginAuthorization{}).Error; err != nil {
		t.Fatal(err)
	}
	// Simulate the earlier split install/migrate workflow before host recovery.
	for _, table := range []string{"plugin_data", "plugin_migrations"} {
		if err := db.Exec("DELETE FROM "+table+" WHERE plugin_id = ?", v.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	m.mu.Lock()
	err = m.recover()
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	current, err := m.load(v.ID)
	if err != nil || !current.Enabled || current.State != "active" || !current.Admission.Accepted || current.Data.Version != 1 || current.Data.MigrationRequired {
		t.Fatal("host required manual admission on recovery", current.State, err)
	}
	m.options.TrustedPublishers = map[string]string{}
	m.mu.Lock()
	err = m.recover()
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	current, _ = m.load(v.ID)
	if current.State != "failed" {
		t.Fatal("recovery did not recheck publisher trust")
	}
}
