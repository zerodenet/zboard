package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Repackage a test archive with another publisher label. The verified key and
// signed manifest stay identical; no installed archive is modified.
func withPublisher(t testing.TB, raw []byte, publisher string) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for _, f := range reader.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if f.Name == "signature.json" {
			var signature Signature
			if err := json.Unmarshal(data, &signature); err != nil {
				t.Fatal(err)
			}
			signature.KeyID = publisher
			data, _ = json.Marshal(signature)
		}
		w, err := writer.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func confirmImport(t testing.TB, m *Manager, raw []byte) Installation {
	t.Helper()
	preview, err := m.PreviewImport(raw, "")
	if err != nil {
		t.Fatal(err)
	}
	tokens := []string{}
	if preview.PublisherMigration != nil {
		tokens = append(tokens, preview.PublisherMigration.Confirmation)
	}
	installed, err := m.ImportConfirmed(raw, "administrator", "", preview.Digest, preview.Fingerprint, tokens...)
	if err != nil {
		t.Fatal(err)
	}
	return installed
}

func TestSameKeyPublisherMigrationAndRetainedReinstallPreserveData(t *testing.T) {
	for _, mode := range []string{"active", "disabled", "uninstalled", "same-identity-reinstall"} {
		t.Run(mode, func(t *testing.T) {
			pub, priv, _ := ed25519.GenerateKey(rand.Reader)
			raw := dataPackage(t, priv, pub, "example.retained", 1, nil)
			oldRaw := withPublisher(t, raw, "local.publisher")
			keys := map[string]string{}
			if mode == "disabled" {
				keys["local.publisher"] = base64.StdEncoding.EncodeToString(pub)
			}
			m, db, opts := testManager(t, keys)
			old := confirmImport(t, m, oldRaw)
			if _, err := m.SaveConfig(context.Background(), old.ID, "administrator", 0, []byte(`{"channel":"retained-secret"}`)); err != nil {
				t.Fatal(err)
			}
			old, _ = m.load(old.ID)
			if mode == "active" {
				var err error
				old, err = m.Action(context.Background(), old.ID, "enable", "administrator", old.Generation, false, "")
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode == "uninstalled" || mode == "same-identity-reinstall" {
				var err error
				old, err = m.Action(context.Background(), old.ID, "uninstall", "administrator", old.Generation, false, "")
				if err != nil {
					t.Fatal(err)
				}
			}
			beforeData, _ := m.readData(old.ID)
			beforeMigrations, _ := m.Migrations(old.ID)
			token, err := m.CreateSession(old.ID, "settings", "admin", 1, true, true)
			if mode == "active" || mode == "disabled" {
				if err != nil {
					t.Fatal(err)
				}
			}
			candidate := raw
			if mode == "same-identity-reinstall" {
				candidate = oldRaw
			}
			preview, err := m.PreviewImport(candidate, "")
			if err != nil {
				t.Fatal(err)
			}
			if (preview.PublisherMigration != nil) != (mode != "same-identity-reinstall") {
				t.Fatal("wrong migration preview", preview)
			}
			if preview.PublisherMigration != nil {
				if preview.PublisherMigration.FromPublisher != old.Publisher || preview.PublisherMigration.ToPublisher != "test.publisher" || preview.PublisherMigration.PreviousState != old.State {
					t.Fatal(preview)
				}
				if _, err := m.ImportConfirmed(candidate, "administrator", "", preview.Digest, preview.Fingerprint); err == nil {
					t.Fatal("publisher silently migrated")
				}
			}
			next := confirmImport(t, m, candidate)
			afterData, _ := m.readData(next.ID)
			afterMigrations, _ := m.Migrations(next.ID)
			if !reflect.DeepEqual(beforeData, afterData) || !reflect.DeepEqual(beforeMigrations, afterMigrations) || next.ConfigCiphertext != old.ConfigCiphertext || next.ConfigRevision != old.ConfigRevision {
				t.Fatal("retained data or history changed")
			}
			if next.Enabled != (mode == "active") || next.Generation != old.Generation+1 || next.State == "uninstalled" || paymentIdentity(old) != paymentIdentity(next) || next.SigningKey != old.SigningKey {
				t.Fatal("identity or activation changed", next)
			}
			if mode == "active" || mode == "disabled" {
				if _, err := m.CheckSession(token.Token, 1, true); err == nil {
					t.Fatal("old session survived migration")
				}
			}
			if _, err := m.packageFor(next); err != nil {
				t.Fatal(err)
			}
			stored, err := os.ReadFile(filepath.Join(opts.Directory, "versions", next.Digest, "package.zbplugin"))
			if err != nil || !bytes.Equal(stored, candidate) {
				t.Fatal("candidate archive rewritten", err)
			}
			var version model.PluginVersion
			if err := db.First(&version, "id = ?", next.VersionID).Error; err != nil || version.Publisher != next.Publisher {
				t.Fatal("version publisher inconsistent", err)
			}
			if mode != "same-identity-reinstall" {
				var op model.PluginOperation
				if err := db.Where("plugin_id = ? AND action = ?", next.ID, "publisher_migration").First(&op).Error; err != nil {
					t.Fatal(err)
				}
				if op.Actor != "administrator" || op.State != "succeeded" || op.PreviousPublisher != old.Publisher || op.Publisher != next.Publisher || op.PackageDigest != next.Digest || op.SigningKeyFingerprint != keyFingerprint(next.SigningKey) {
					t.Fatal("incomplete migration audit", op)
				}
			}
			m.Close()
			restarted, err := NewManager(db, m.cipher, opts, "v0.0.1")
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			current, err := restarted.load(next.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := restarted.packageFor(current); err != nil {
				t.Fatal("migration did not survive restart", err)
			}
		})
	}
}

func TestPublisherMigrationFailureKeepsSignedPackageAndHistory(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	raw := dataPackage(t, priv, pub, "example.retained", 1, nil)
	m, db, opts := testManager(t, nil)
	oldRaw := withPublisher(t, raw, "local.publisher")
	old := confirmImport(t, m, oldRaw)
	old, err := m.Action(context.Background(), old.ID, "enable", "administrator", old.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := m.readData(old.ID)
	history, _ := m.Migrations(old.ID)
	nextRaw := dataPackage(t, priv, pub, old.ID, 2, []DataChange{{Target: "storage", Operation: "rename", Key: "legacy", To: "current"}})
	preview, err := m.PreviewImport(nextRaw, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_publisher BEFORE UPDATE OF publisher ON plugin_installations BEGIN SELECT RAISE(ABORT, 'injected identity failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := m.ImportConfirmed(nextRaw, "administrator", "", preview.Digest, preview.Fingerprint, preview.PublisherMigration.Confirmation); err == nil {
		t.Fatal("ignored commit failure")
	}
	current, _ := m.load(old.ID)
	after, _ := m.readData(old.ID)
	records, _ := m.Migrations(old.ID)
	if !reflect.DeepEqual(current.PluginInstallation, old.PluginInstallation) || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(history, records) {
		t.Fatal("failed migration changed committed state")
	}
	oldArchive, err := os.ReadFile(filepath.Join(opts.Directory, "versions", old.Digest, "package.zbplugin"))
	if err != nil || !bytes.Equal(oldArchive, oldRaw) {
		t.Fatal("old signed archive changed", err)
	}
	if _, err := os.Stat(filepath.Join(opts.Directory, "versions", preview.Digest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed candidate retained", err)
	}
	operations, _ := m.Operations(old.ID)
	if operations[0].State != "failed" || operations[0].PreviousPublisher != old.Publisher || operations[0].PackageDigest != preview.Digest {
		t.Fatal("failure audit lost", operations)
	}
	if err := db.Exec(`DROP TRIGGER fail_publisher`).Error; err != nil {
		t.Fatal(err)
	}
	next := confirmImport(t, m, nextRaw)
	if !next.Enabled || next.Data.Version != 2 || next.Publisher != "test.publisher" {
		t.Fatal("retry did not complete", next)
	}
	// Recovery by reimporting an older signed package must still satisfy the
	// current data schema. Publisher migration is not a rollback bypass.
	oldPreview, err := m.PreviewImport(oldRaw, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ImportConfirmed(oldRaw, "administrator", "", oldPreview.Digest, oldPreview.Fingerprint, oldPreview.PublisherMigration.Confirmation); err == nil {
		t.Fatal("publisher migration bypassed incompatible data downgrade")
	}
	retained, _ := m.load(next.ID)
	if retained.Generation != next.Generation || retained.Publisher != next.Publisher || retained.Digest != next.Digest {
		t.Fatal("rejected downgrade changed current installation")
	}
}

func TestPublisherMigrationRejectsStaleConcurrentAndConflictingIdentity(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	raw := fixtureSignedPackage(t, priv, pub, nil)
	m, db, _ := testManager(t, nil)
	old := confirmImport(t, m, withPublisher(t, raw, "local.publisher"))
	preview, err := m.PreviewImport(raw, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ImportConfirmed(raw, "administrator", "", preview.Digest, "wrong", preview.PublisherMigration.Confirmation); !errors.Is(err, ErrConflict) {
		t.Fatal("wrong fingerprint accepted", err)
	}
	if _, err := m.SaveConfig(context.Background(), old.ID, "administrator", 0, []byte(`{"changed":true}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ImportConfirmed(raw, "administrator", "", preview.Digest, preview.Fingerprint, preview.PublisherMigration.Confirmation); !errors.Is(err, ErrConflict) {
		t.Fatal("stale confirmation accepted", err)
	}
	preview, _ = m.PreviewImport(raw, "")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.ImportConfirmed(raw, "administrator", "", preview.Digest, preview.Fingerprint, preview.PublisherMigration.Confirmation)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("duplicate migration committed", success, conflict)
	}
	current, _ := m.load(old.ID)
	pack, _ := m.inspectPackage(raw, "")
	if err := db.Model(&model.PluginInstallation{}).Where("id = ?", old.ID).Update("generation", gorm.Expr("generation + 1")).Error; err != nil {
		t.Fatal(err)
	}
	if err := m.commitCandidate(context.Background(), current.PluginInstallation, pack, "administrator", nil); !errors.Is(err, ErrConflict) {
		t.Fatal("transaction overwrote concurrent installation", err)
	}
}

func TestPublisherMigrationKeepsPinTrustAndPackageValidation(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	raw := fixtureSignedPackage(t, priv, pub, nil)
	m, _, _ := testManager(t, nil)
	confirmImport(t, m, withPublisher(t, raw, "local.publisher"))
	changedPub, changedPriv, _ := ed25519.GenerateKey(rand.Reader)
	changed := fixtureSignedPackage(t, changedPriv, changedPub, nil)
	if _, err := m.PreviewImport(changed, ""); err == nil {
		t.Fatal("key rotation admitted")
	}
	for _, publisher := range []string{"local.publisher", "test.publisher"} {
		m.options.TrustedPublishers = map[string]string{publisher: base64.StdEncoding.EncodeToString(changedPub)}
		if _, err := m.PreviewImport(raw, ""); err == nil {
			t.Fatal("host trust conflict ignored", publisher)
		}
	}
	m.options.TrustedPublishers = nil
	preview, _ := m.PreviewImport(raw, "")
	other := fixtureSignedPackage(t, priv, pub, func(m *Manifest, _ map[string][]byte) { m.ID = "example.other" })
	otherPreview, _ := m.PreviewImport(other, "")
	if _, err := m.ImportConfirmed(other, "administrator", "", otherPreview.Digest, otherPreview.Fingerprint, preview.PublisherMigration.Confirmation); !errors.Is(err, ErrConflict) {
		t.Fatal("migration transferred to another plugin", err)
	}
	if _, err := m.PreviewImport(raw, base64.StdEncoding.EncodeToString(changedPub)); err == nil {
		t.Fatal("invalid signature admitted")
	}
	// A trusted market's legacy direct-install path supplies generic trust, but
	// no publisher-specific confirmation and must still reject the migration.
	pack, _ := m.inspectPackage(raw, "")
	if _, err := m.importVerified(raw, "administrator", pack, true); err == nil {
		t.Fatal("trusted source bypassed migration confirmation")
	}
}

func TestTrustedMarketRequiresExplicitSameKeyPublisherMigrationForReinstall(t *testing.T) {
	fixture := newPublicMarketFixture(t)
	m := fixture.manager
	old := confirmImport(t, m, withPublisher(t, fixture.packageRaw, "local.publisher"))
	var err error
	old, err = m.Action(context.Background(), old.ID, "uninstall", "administrator", old.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := m.PreviewMarket(context.Background(), old.ID, "1.0.0")
	if err != nil || !preview.Trusted || preview.PublisherMigration == nil {
		t.Fatal("market preflight blocked migration", preview, err)
	}
	for _, args := range [][2]string{{"", ""}, {preview.Fingerprint, ""}, {"", preview.PublisherMigration.Confirmation}, {preview.Fingerprint, "stale"}} {
		if _, err := m.InstallMarketConfirmed(context.Background(), old.ID, "1.0.0", preview.Digest, args[0], "administrator", args[1]); err == nil {
			t.Fatal("market bypassed exact migration consent", args)
		}
	}
	next, err := m.InstallMarketConfirmed(context.Background(), old.ID, "1.0.0", preview.Digest, preview.Fingerprint, "administrator", preview.PublisherMigration.Confirmation)
	if err != nil || next.Publisher != "test.publisher" || next.Enabled || paymentIdentity(next) != paymentIdentity(old) {
		t.Fatal("market reinstall changed identity or activation", next, err)
	}
}
