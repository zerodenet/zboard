package plugins

import (
	"context"
	"strings"
	"testing"
)

const registryFixture = `{"schema_version":2,"host":"zboard","plugins":[{"id":"zboard.oauth","repository":"https://github.com/higanbana986/zboard-oauth","publisher":{"id":"higanbana986","public_key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},"name":"OAuth from directory","description":"Directory description","license":"MPL-2.0","maintainers":["higanbana986"],"release_source":{"type":"github-releases","metadata_asset":"marketplace-entry.json"},"surfaces":["public","account","admin"],"capabilities":["zboard.ui.page.v1","zboard.identity.provider.v1"]}]}`

func TestDefaultMarketReadsAdmissionWithoutCopyingReleaseVersions(t *testing.T) {
	m, db, _ := testManager(t, nil)
	calls := 0
	m.fetch = func(_ context.Context, url string, limit int64) ([]byte, error) {
		calls++
		switch url {
		case DefaultRegistryURL:
			if limit != 2<<20 {
				t.Fatal(limit)
			}
			return []byte(registryFixture), nil
		default:
			t.Fatal(url, limit)
		}
		return nil, nil
	}
	market, err := m.Market(context.Background())
	if err != nil || market.Kind != "registry" || len(market.Entries) != 1 {
		t.Fatal(market, err)
	}
	entry := market.Entries[0]
	if entry.ID != "zboard.oauth" || entry.Name != "OAuth from directory" || entry.Description != "Directory description" ||
		entry.Version != "" || entry.PackageURL != "" ||
		entry.PublicKey == "" || entry.ReleaseSource.Type != "github-releases" ||
		len(entry.Capabilities) != 2 || entry.DiscoveryOnly {
		t.Fatal(entry)
	}
	if calls != 1 {
		t.Fatal("market did not fetch only the directory", calls)
	}
	var count int64
	db.Table("plugin_installations").Count(&count)
	if count != 0 {
		t.Fatal("directory lookup installed a plugin")
	}
}

func TestPublicRegistryRejectsWrongHostUnsafeLinksAndInvalidBoundaries(t *testing.T) {
	for _, raw := range []string{
		strings.Replace(registryFixture, `"host":"zboard"`, `"host":"znet-sink"`, 1),
		strings.Replace(registryFixture, "https://github.com/higanbana986/zboard-oauth", "javascript:alert(1)", 1),
		strings.Replace(registryFixture, "https://github.com/higanbana986/zboard-oauth", "https://127.0.0.1/plugin", 1),
		strings.Replace(registryFixture, "marketplace-entry.json", "../entry.json", 1),
		strings.Replace(registryFixture, `"name":"OAuth from directory"`, `"name":""`, 1),
		strings.Replace(registryFixture, `"maintainers":["higanbana986"]`, `"maintainers":[]`, 1),
		strings.Replace(registryFixture, "zboard.ui.page.v1", "znet-sink.shell.v1", 1),
		strings.Replace(registryFixture, `"public","account","admin"`, `"public","unknown"`, 1),
		strings.Replace(registryFixture, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "bad-key", 1),
	} {
		if _, err := parseRegistry([]byte(raw)); err == nil {
			t.Fatal("invalid registry accepted", raw)
		}
	}
}
