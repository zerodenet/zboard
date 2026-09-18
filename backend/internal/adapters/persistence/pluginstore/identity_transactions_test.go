package pluginstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	identitycap "github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"gorm.io/gorm"
)

func identityTransactionFixture(t *testing.T) (*gorm.DB, IdentityTransactions, plugins.IdentityFence) {
	t.Helper()
	db, err := datastore.OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "identity.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	lease := model.PluginHostLease{ID: 1, Owner: "host-one", Epoch: 3, ExpiresAt: time.Now().UTC().Add(time.Minute)}
	installation := model.PluginInstallation{ID: "example.identity", VersionID: "version", Name: "Identity", Publisher: "trusted", State: "active", Enabled: true, Generation: 4, ConfigRevision: 2}
	if err := db.Create(&lease).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&installation).Error; err != nil {
		t.Fatal(err)
	}
	store := IdentityTransactions{
		DB:         db,
		Tokens:     identitycap.SessionTokens{Key: []byte("0123456789abcdef0123456789abcdef")},
		EmailCodes: identitycap.EmailCodes{Key: []byte("0123456789abcdef0123456789abcdef")},
	}
	fence := plugins.IdentityFence{InstallationID: installation.ID, Publisher: installation.Publisher, Generation: installation.Generation, Revision: installation.ConfigRevision, HostOwner: lease.Owner, HostEpoch: lease.Epoch}
	return db, store, fence
}

func TestIdentityTransactionsFenceHostAndInstallation(t *testing.T) {
	_, store, fence := identityTransactionFixture(t)
	called := false
	if err := store.WithinIdentity(context.Background(), fence, func(plugins.IdentityServices) error { called = true; return nil }); err != nil || !called {
		t.Fatalf("valid fence called=%t err=%v", called, err)
	}
	stale := fence
	stale.Revision++
	if err := store.WithinIdentity(context.Background(), stale, func(plugins.IdentityServices) error { t.Fatal("stale installation committed"); return nil }); !errors.Is(err, plugins.ErrConflict) {
		t.Fatalf("stale installation error=%v", err)
	}
	stale = fence
	stale.HostEpoch++
	if err := store.WithinIdentity(context.Background(), stale, func(plugins.IdentityServices) error { t.Fatal("stale host committed"); return nil }); !errors.Is(err, plugins.ErrUnavailable) {
		t.Fatalf("stale host error=%v", err)
	}
}

func TestIdentityTransactionsRollbackCoreIdentityWrite(t *testing.T) {
	db, store, fence := identityTransactionFixture(t)
	user := model.User{Email: "owner@example.test", Password: "confirmed-hash", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	want := errors.New("reject completion")
	err := store.WithinIdentity(context.Background(), fence, func(services plugins.IdentityServices) error {
		_, err := services.External.Resolve(context.Background(), identitycap.ExternalEvidence{
			Authority: "example.identity", Publisher: "trusted", Issuer: "https://issuer.example", Subject: "subject",
		}, identitycap.BindingConfirmation{AccountID: user.ID, PasswordHash: user.Password})
		if err != nil {
			return err
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("rollback error=%v", err)
	}
	var bindings, audits int64
	if err := db.Model(&model.ExternalIdentity{}).Count(&bindings).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AuditLog{}).Count(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if bindings != 0 || audits != 0 {
		t.Fatalf("rolled back bindings=%d audits=%d", bindings, audits)
	}
}
