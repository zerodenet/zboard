package plugins

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestMarketInstallVerifiesCatalogSelectionAndDownloadedIdentity(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	raw := fixtureSignedPackage(t, priv, pub, nil)
	keys := map[string]string{"test.publisher": base64.StdEncoding.EncodeToString(pub)}
	m, _, _ := testManager(t, keys)
	m.options.CatalogURL = "https://market.example/catalog.json"
	digest := sha256.Sum256(raw)
	entry := MarketEntry{ID: "example.welcome", Name: "Welcome", Version: "1.0.0", Publisher: "test.publisher", SHA256: hex.EncodeToString(digest[:]), PackageURL: "https://market.example/welcome.zbplugin", Surfaces: []string{"public"}}
	catalog := func() []byte {
		payload, _ := json.Marshal(marketPayload{SchemaVersion: 1, ExpiresAt: time.Now().Add(time.Hour), Entries: []MarketEntry{entry}})
		signed, _ := json.Marshal(signedCatalog{Payload: payload, Signature: Signature{Algorithm: "ed25519", KeyID: "test.publisher", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))}})
		return signed
	}
	m.fetch = func(_ context.Context, url string, _ int64) ([]byte, error) {
		if url == m.options.CatalogURL {
			return catalog(), nil
		}
		if url == entry.PackageURL {
			return raw, nil
		}
		return nil, errors.New("unexpected URL")
	}
	if _, err := m.InstallMarket(context.Background(), entry.ID, "stale", "admin"); !errors.Is(err, ErrConflict) {
		t.Fatal("stale selection accepted", err)
	}
	item, err := m.InstallMarket(context.Background(), entry.ID, entry.SHA256, "admin")
	if err != nil || item.Enabled {
		t.Fatal(item, err)
	}
	entry.Version = "2.0.0"
	if _, err := m.InstallMarket(context.Background(), entry.ID, entry.SHA256, "admin"); err == nil {
		t.Fatal("identity mismatch accepted")
	}
	entry.Version = "1.0.0"
	raw = append(raw, 0)
	if _, err := m.InstallMarket(context.Background(), entry.ID, entry.SHA256, "admin"); err == nil {
		t.Fatal("download checksum mismatch accepted")
	}
}
