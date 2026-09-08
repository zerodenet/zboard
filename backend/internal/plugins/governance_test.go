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

func TestPluginAuthorizationDefaultsDenyAndRevokesSessions(t *testing.T) {
	raw, keys := fixturePackage(t, nil)
	m, _, _ := testManager(t, keys)
	v, err := m.Import(raw, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if v.Authorization.Reviewed {
		t.Fatal("new package implicitly authorized")
	}
	if _, err = m.Action(context.Background(), v.ID, "enable", "admin", v.Generation, false, ""); !errors.Is(err, ErrPermission) {
		t.Fatal("unauthorized enable", err)
	}
	if _, err = m.CreateSession(v.ID, "settings", "admin", 1, true, true); err == nil {
		t.Fatal("unreviewed config page accessible")
	}
	if _, err = m.Authorize(v.ID, "admin", v.Digest, v.Generation, []string{"zboard.core.sql.v1"}, false); err == nil {
		t.Fatal("undeclared core capability granted")
	}
	v, err = m.Authorize(v.ID, "admin", v.Digest, v.Generation, v.Manifest.Capabilities, false)
	if err != nil {
		t.Fatal(err)
	}
	session, err := m.CreateSession(v.ID, "settings", "admin", 1, true, true)
	if err != nil {
		t.Fatal(err)
	}
	stale := v.Generation
	v, err = m.Authorize(v.ID, "admin", v.Digest, v.Generation, []string{PageCapability}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.CheckSession(session.Token, 1, true); err == nil {
		t.Fatal("old session survived permission change")
	}
	if _, err = m.SaveConfig(context.Background(), v.ID, "admin", 0, []byte(`{}`)); err == nil {
		t.Fatal("revoked config permission ignored")
	}
	if _, err = m.Authorize(v.ID, "admin", v.Digest, stale, v.Manifest.Capabilities, false); !errors.Is(err, ErrConflict) {
		t.Fatal("stale authorization accepted")
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
func migratePlugin(t testing.TB, m *Manager, v Installation) Installation {
	t.Helper()
	next, err := m.Action(context.Background(), v.ID, "migrate", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	return next
}
func TestPluginPrivateDataIsolationCASAndUninstallRetention(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	keys := map[string]string{"test.publisher": base64.StdEncoding.EncodeToString(pub)}
	m, db, _ := testManager(t, keys)
	a, err := importApproved(t, m, dataPackage(t, priv, pub, "example.first", 1, nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Action(context.Background(), a.ID, "enable", "admin", a.Generation, false, ""); err == nil {
		t.Fatal("enabled before migration")
	}
	a = migratePlugin(t, m, a)
	b, err := importApproved(t, m, dataPackage(t, priv, pub, "example.second", 1, nil))
	if err != nil {
		t.Fatal(err)
	}
	b = migratePlugin(t, m, b)
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
}
func TestPluginMigrationTransactionUpgradeAndIncompatibleRollback(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	keys := map[string]string{"test.publisher": base64.StdEncoding.EncodeToString(pub)}
	m, db, _ := testManager(t, keys)
	ctx := context.Background()
	old, err := importApproved(t, m, dataPackage(t, priv, pub, "example.data", 1, nil))
	if err != nil {
		t.Fatal(err)
	}
	old = migratePlugin(t, m, old)
	nextRaw := dataPackage(t, priv, pub, old.ID, 2, []DataChange{{Target: "storage", Operation: "rename", Key: "legacy", To: "current"}, {Target: "config", Operation: "set_default", Key: "new_option", Value: json.RawMessage(`true`)}})
	next, err := m.Import(nextRaw, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if next.Authorization.Reviewed {
		t.Fatal("upgrade silently retained authorization")
	}
	next, err = m.Authorize(next.ID, "admin", next.Digest, next.Generation, next.Manifest.Capabilities, false)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := m.readData(next.ID)
	if err := db.Exec(`CREATE TRIGGER fail_migration BEFORE INSERT ON plugin_migrations BEGIN SELECT RAISE(ABORT, 'injected failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = m.Action(ctx, next.ID, "migrate", "admin", next.Generation, false, ""); err == nil {
		t.Fatal("migration failure ignored")
	}
	after, _ := m.readData(next.ID)
	if after.Ciphertext != before.Ciphertext || after.Version != before.Version {
		t.Fatal("failed migration leaked data")
	}
	view, _ := m.Config(next.ID)
	if view.Configured {
		t.Fatal("failed migration leaked config")
	}
	records, _ := m.Migrations(next.ID)
	if len(records) != 1 {
		t.Fatal("failed migration left applied record")
	}
	if err := db.Exec(`DROP TRIGGER fail_migration`).Error; err != nil {
		t.Fatal(err)
	}
	next = migratePlugin(t, m, next)
	if next.Data.Version != 2 || next.ConfigRevision != 1 || next.Enabled {
		t.Fatal("migration did not commit atomically")
	}
	if _, err = m.Action(ctx, next.ID, "rollback", "admin", next.Generation, false, old.VersionID); err == nil {
		t.Fatal("incompatible old program restored")
	}
	// A package may never rewrite an already applied migration under the same version.
	tampered := fixtureSignedPackage(t, priv, pub, func(manifest *Manifest, files map[string][]byte) {
		pack, e := ReadPackage(nextRaw, keys)
		if e != nil {
			t.Fatal(e)
		}
		*manifest = pack.Manifest
		manifest.Data.Migrations[0].Changes[0].Value = json.RawMessage(`"changed"`)
	})
	next, err = importApproved(t, m, tampered)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Action(ctx, next.ID, "enable", "admin", next.Generation, false, ""); err == nil {
		t.Fatal("modified applied migration accepted")
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

func TestPluginRecoveryWithoutAuthorizationStopsInsteadOfRunning(t *testing.T) {
	raw, keys := fixturePackage(t, nil)
	m, db, _ := testManager(t, keys)
	v, err := importApproved(t, m, raw)
	if err != nil {
		t.Fatal(err)
	}
	v, err = m.Action(context.Background(), v.ID, "enable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	// A pre-governance installation has desired enabled state but no package review.
	if err := db.Where("plugin_id = ?", v.ID).Delete(&model.PluginAuthorization{}).Error; err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	err = m.recover()
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	current, err := m.load(v.ID)
	if err != nil || current.Enabled || current.State != "disabled" || current.LastError != "" {
		t.Fatal("missing authorization treated as runtime failure", current.State, err)
	}
}
