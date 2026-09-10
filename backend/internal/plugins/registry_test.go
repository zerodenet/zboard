package plugins

import (
	"context"
	"strings"
	"testing"
)

const registryFixture = `{"schema_version":1,"host":"zboard","plugins":[{"id":"zboard.oauth","name":"OAuth","description":"Third-party login","repository":"https://github.com/higanbana986/zboard-oauth","publisher":{"id":"higanbana986","public_key":null},"source":{"version":"v0.0.1"},"releases":[]}]}`

func TestDefaultMarketDiscoversSourceOnlyPluginsWithoutGrantingInstallTrust(t *testing.T) {
	m, db, _ := testManager(t, nil)
	calls := 0
	m.fetch = func(_ context.Context, url string, limit int64) ([]byte, error) {
		calls++
		if url != DefaultRegistryURL || limit != 2<<20 {
			t.Fatal(url, limit)
		}
		return []byte(registryFixture), nil
	}
	market, err := m.Market(context.Background())
	if err != nil || market.Kind != "registry" || len(market.Entries) != 1 {
		t.Fatal(market, err)
	}
	e := market.Entries[0]
	if e.ID != "zboard.oauth" || !e.DiscoveryOnly || e.PackageURL != "" || e.PublicKey != "" || e.Version != "0.0.1" {
		t.Fatal(e)
	}
	if _, err := m.InstallMarket(context.Background(), e.ID, "", "admin"); err == nil {
		t.Fatal("discovery metadata authorized installation")
	}
	if calls != 1 {
		t.Fatal("attempted package download")
	}
	var count int64
	db.Table("plugin_installations").Count(&count)
	if count != 0 {
		t.Fatal("discovery installed a plugin")
	}
}

func TestPublicRegistryRejectsWrongHostAndUnsafeLinks(t *testing.T) {
	for _, raw := range []string{
		strings.Replace(registryFixture, `"host":"zboard"`, `"host":"znet-sink"`, 1),
		strings.Replace(registryFixture, "https://github.com/higanbana986/zboard-oauth", "javascript:alert(1)", 1),
		strings.Replace(registryFixture, "https://github.com/higanbana986/zboard-oauth", "https://127.0.0.1/plugin", 1),
	} {
		if _, err := parseRegistry([]byte(raw)); err == nil {
			t.Fatal("invalid registry accepted")
		}
	}
}
