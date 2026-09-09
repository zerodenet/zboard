package plugins

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestSignedMarketDelegatesOnlyTheSelectedPluginKey(t *testing.T) {
	rootPub, rootPriv, _ := ed25519.GenerateKey(rand.Reader)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	raw := fixtureSignedPackage(t, priv, pub, nil)
	m, _, _ := testManager(t, map[string]string{"market.root": base64.StdEncoding.EncodeToString(rootPub)})
	m.options.CatalogURL = "https://market.example/catalog.json"
	sum := sha256.Sum256(raw)
	entry := MarketEntry{ID: "example.welcome", Name: "Welcome", Version: "1.0.0", Publisher: "test.publisher", PublicKey: base64.StdEncoding.EncodeToString(pub), SHA256: hex.EncodeToString(sum[:]), PackageURL: "https://github.com/example/plugin/releases/download/v1.0.0/plugin.zbplugin"}
	m.fetch = func(_ context.Context, url string, _ int64) ([]byte, error) {
		if url != m.options.CatalogURL {
			return raw, nil
		}
		payload, _ := json.Marshal(marketPayload{SchemaVersion: 1, ExpiresAt: time.Now().Add(time.Hour), Entries: []MarketEntry{entry}})
		return json.Marshal(signedCatalog{Payload: payload, Signature: Signature{Algorithm: "ed25519", KeyID: "market.root", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(rootPriv, payload))}})
	}
	v, err := m.InstallMarket(context.Background(), entry.ID, entry.SHA256, "admin")
	if err != nil || v.Enabled || !v.LocalTrust {
		t.Fatal(v, err)
	}
	if _, err := m.packageFor(v); err != nil {
		t.Fatal(err)
	}
	other := fixtureSignedPackage(t, priv, pub, func(m *Manifest, _ map[string][]byte) { m.ID = "example.other" })
	if _, err := m.Import(other, "admin"); err == nil {
		t.Fatal("market key trusted globally")
	}
	// A valid catalog signature cannot authorize an artifact signed by another key.
	changedPub, _, _ := ed25519.GenerateKey(rand.Reader)
	entry.PublicKey = base64.StdEncoding.EncodeToString(changedPub)
	if _, err := m.InstallMarket(context.Background(), entry.ID, entry.SHA256, "admin"); err == nil {
		t.Fatal("wrong catalog key accepted")
	}
}

func TestMarketRedirectsRequireBoundedPublicHTTPS(t *testing.T) {
	for _, url := range []string{"http://example.com/p", "https://127.0.0.1/p", "https://10.0.0.1/p", "https://user:pass@example.com/p", "https://example.com:8443/p"} {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		if validateMarketRedirect(req, nil) == nil {
			t.Fatal("unsafe redirect", url)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, "https://release-assets.githubusercontent.com/p?token=temporary", nil)
	req.Header.Set("Authorization", "private")
	if err := validateMarketRedirect(req, nil); err != nil || req.Header.Get("Authorization") != "" {
		t.Fatal(err)
	}
	if err := validateMarketRedirect(req, make([]*http.Request, 5)); err == nil {
		t.Fatal("redirect loop accepted")
	}
}
