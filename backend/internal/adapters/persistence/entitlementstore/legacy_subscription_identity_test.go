package entitlementstore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLegacySubscriptionIdentityPreservesExistingTokenFormats(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "legacy-token.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	user := model.User{Email: "legacy@example.test", Password: "unused", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"old-token", strings.Repeat("a", 32), strings.Repeat("b", 43)} {
		sub := model.Subscription{UserID: user.ID, EndAt: time.Now().Add(time.Hour), Status: "active"}
		if err := db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
		row := model.SubscriptionToken{UserID: user.ID, SubscriptionID: &sub.ID, TokenHash: entitlements.HashAccessToken(raw), TokenPrefix: raw[:8]}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		if ok, err := LegacySubscriptionIdentity(db, t.Context(), " "+raw+" "); err != nil || !ok {
			t.Fatal("existing token rejected", len(raw), ok, err)
		}
		if err := db.Model(&row).Update("revoked_at", time.Now()).Error; err != nil {
			t.Fatal(err)
		}
		if ok, err := LegacySubscriptionIdentity(db, t.Context(), raw); err != nil || ok {
			t.Fatal("revoked token allowed", ok, err)
		}
	}
	if ok, err := LegacySubscriptionIdentity(db, t.Context(), "random-unknown-token"); err != nil || ok {
		t.Fatal("probe allowed", ok, err)
	}
}
