package plugins

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/identitystore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestTwoPluginsShareScopedBusinessCapabilities(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	makePackage := func(id string, quota bool) []byte {
		return fixtureSignedPackage(t, priv, pub, func(m *Manifest, files map[string][]byte) {
			m.ID = id
			m.Capabilities = append(m.Capabilities, UISlotCapability, AccountSelfReadCapability, SubscriptionReadCapability, MessageReadCapability)
			if quota {
				m.Capabilities = append(m.Capabilities, AccountAdminReadCapability, SubscriptionConfigReadCapability, SubscriptionAdminReadCapability, SubscriptionQuotaWriteCapability, SubscriptionTermWriteCapability, SubscriptionStatusWriteCapability, MessageAckCapability)
			}
			m.Contributions.Pages = append(m.Contributions.Pages, Page{ID: "console", Surface: "admin", Purpose: "business", Title: "Console"})
			m.Contributions.Slots = append(m.Contributions.Slots, Slot{ID: "overview", Surface: "account", Slot: "account.overview.cards", Title: "Overview", Entrypoint: "ui/card.html"})
			files["ui/card.html"] = []byte("<!doctype html><title>Overview</title>")
		})
	}
	first := makePackage("example.reader", false)
	second := makePackage("example.operator", true)
	m, db, _ := testManager(t, map[string]string{"test.publisher": base64.StdEncoding.EncodeToString(pub)})
	for _, user := range []model.User{
		{ID: 1, Email: "owner@example.test", Password: "unused", Status: "active"},
		{ID: 2, Email: "other@example.test", Password: "unused", Status: "active"},
		{ID: 3, Email: "admin@example.test", Password: "unused", Status: "active", IsAdmin: true},
	} {
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
	}
	accounts := identity.Accounts{Repository: identitystore.Accounts{DB: db}}
	for _, raw := range [][]byte{first, second} {
		v, err := importFixture(t, m, raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.Action(context.Background(), v.ID, "enable", "admin", v.Generation, false, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"example.reader", "example.operator"} {
		session, err := m.CreateSession(id, "home", "account", 1, false, false)
		if err != nil {
			t.Fatal(err)
		}
		authority := SessionAuthority{Manager: m, Accounts: accounts, UserID: 1}
		proof := catalog.Credential{Kind: "plugin_session", Proof: session.Token}
		for _, operation := range []string{"account.self.get", "subscriptions.owned.list", "subscriptions.owned.get", "messages.owned.list"} {
			grant, err := authority.Resolve(context.Background(), proof, operation)
			if err != nil || grant.Principal.AccountID != 1 || grant.Administrative {
				t.Fatalf("%s %s: %+v %v", id, operation, grant, err)
			}
		}
		_, configErr := authority.Resolve(context.Background(), proof, "subscriptions.owned.config")
		if id == "example.reader" && !errors.Is(configErr, catalog.ErrDenied) {
			t.Fatalf("summary read escalated to config: %v", configErr)
		}
		if id == "example.operator" && configErr != nil {
			t.Fatalf("separate config grant unavailable: %v", configErr)
		}
		registry := catalog.New(authority, authority)
		descriptor := catalog.Descriptor{Name: "account.self.get", Version: "1.0", Owner: "identity", Kind: "query", Sensitivity: "account_profile", Authorization: "verified current account", Idempotency: "read_only", Execution: "synchronous", TimeoutMillis: 1000, Quota: "one account", RateLimitPerMinute: 10, ErrorCodes: []string{"permission_denied"}, Compatibility: "v1", Deprecation: "none", InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`)}
		if err := registry.Register(descriptor, func(_ context.Context, grant catalog.Grant, _ json.RawMessage) (any, error) {
			return map[string]uint{"actor_id": grant.Principal.AccountID}, nil
		}); err != nil {
			t.Fatal(err)
		}
		result, err := registry.Invoke(context.Background(), proof, "account.self.get", json.RawMessage(`{}`))
		if err != nil || result.(map[string]uint)["actor_id"] != 1 {
			t.Fatalf("%s could not invoke shared capability: %v %+v", id, err, result)
		}
		for _, operation := range []string{"account.admin.get", "subscriptions.admin.list", "subscriptions.quota.adjust", "subscriptions.term.extend", "subscriptions.status.cancel"} {
			if _, err := authority.Resolve(context.Background(), proof, operation); !errors.Is(err, catalog.ErrDenied) {
				t.Fatalf("%s account page escalated %s: %v", id, operation, err)
			}
		}
		other := SessionAuthority{Manager: m, Accounts: accounts, UserID: 2}
		if _, err := other.Resolve(context.Background(), proof, "subscriptions.owned.list"); !errors.Is(err, catalog.ErrDenied) {
			t.Fatalf("%s crossed user: %v", id, err)
		}
		slot, err := m.CreateSlotSession(id, "overview", "account.overview.cards", "account", 1, false, 1)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := authority.Resolve(context.Background(), catalog.Credential{Kind: "plugin_session", Proof: slot.Token}, "account.self.get"); err != nil {
			t.Fatalf("%s slot: %v", id, err)
		}
	}
	adminPage, err := m.CreateSession("example.operator", "console", "admin", 3, true, false)
	if err != nil {
		t.Fatal(err)
	}
	admin := SessionAuthority{Manager: m, Accounts: accounts, UserID: 3, Admin: true}
	adminProof := catalog.Credential{Kind: "plugin_session", Proof: adminPage.Token}
	for _, operation := range []string{"account.admin.get", "subscriptions.admin.list", "subscriptions.quota.adjust", "subscriptions.term.extend", "subscriptions.status.cancel"} {
		grant, err := admin.Resolve(context.Background(), adminProof, operation)
		if err != nil || !grant.Administrative {
			t.Fatalf("admin %s: %+v %v", operation, grant, err)
		}
	}
	readerAdmin, err := m.CreateSession("example.reader", "console", "admin", 3, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Resolve(context.Background(), catalog.Credential{Kind: "plugin_session", Proof: readerAdmin.Token}, "subscriptions.quota.adjust"); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("read-only plugin wrote quota: %v", err)
	}
	if _, err := admin.Resolve(context.Background(), catalog.Credential{Kind: "plugin_session", Proof: readerAdmin.Token}, "subscriptions.term.extend"); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("read-only plugin extended subscription: %v", err)
	}
	if err := db.Model(&model.User{}).Where("id = 3").Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Resolve(context.Background(), adminProof, "subscriptions.quota.adjust"); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("revoked admin remained authorized: %v", err)
	}
	if err := db.Model(&model.User{}).Where("id = 3").Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	installations, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	var v Installation
	for _, candidate := range installations {
		if candidate.ID == "example.operator" {
			v = candidate
		}
	}
	if v.ID == "" {
		t.Fatal("operator installation missing")
	}
	if _, err := m.Action(context.Background(), v.ID, "disable", "admin", v.Generation, false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Resolve(context.Background(), adminProof, "subscriptions.quota.adjust"); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("disabled plugin retained operation: %v", err)
	}
	slots, err := m.Slots("account", "account.overview.cards", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 1 || slots[0].PluginID != "example.reader" {
		t.Fatalf("disabled plugin retained slot: %+v", slots)
	}
	pages, err := m.Pages("admin", 3, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range pages {
		if page.PluginID == "example.operator" {
			t.Fatalf("disabled plugin retained page/menu: %+v", pages)
		}
	}
}
