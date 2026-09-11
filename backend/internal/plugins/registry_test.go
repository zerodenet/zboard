package plugins

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

const registryFixture = `{"schema_version":2,"host":"zboard","plugins":[{"id":"zboard.oauth","repository":"https://github.com/higanbana986/zboard-oauth","publisher":{"id":"higanbana986","public_key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},"metadata_source":{"type":"repository-file","path":"marketplace.json"},"release_source":{"type":"github-releases","metadata_asset":"marketplace-entry.json"},"surfaces":["public","account","admin"],"capabilities":["zboard.ui.page.v1","zboard.identity.provider.v1"]}]}`

const publisherMetadataFixture = `{"schema_version":1,"id":"zboard.oauth","name":"OAuth from repository","description":"Repository description","repository":"https://github.com/higanbana986/zboard-oauth","license":"MPL-2.0","maintainers":["higanbana986"]}`

func metadataAPIResponse(t *testing.T, value string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"type": "file", "encoding": "base64", "size": len(value), "content": base64.StdEncoding.EncodeToString([]byte(value))})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

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
		case "https://api.github.com/repos/higanbana986/zboard-oauth/contents/marketplace.json":
			return metadataAPIResponse(t, publisherMetadataFixture), nil
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
	if entry.ID != "zboard.oauth" || entry.Name != "OAuth from repository" || entry.Description != "Repository description" ||
		entry.Version != "" || entry.PackageURL != "" ||
		entry.PublicKey == "" || entry.ReleaseSource.Type != "github-releases" ||
		len(entry.Capabilities) != 2 || entry.DiscoveryOnly {
		t.Fatal(entry)
	}
	if calls != 2 {
		t.Fatal("market did not fetch exactly the directory and repository metadata", calls)
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
		strings.Replace(registryFixture, "marketplace.json", "manifest.json", 1),
		strings.Replace(registryFixture, "zboard.ui.page.v1", "znet-sink.shell.v1", 1),
		strings.Replace(registryFixture, `"public","account","admin"`, `"public","unknown"`, 1),
		strings.Replace(registryFixture, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "bad-key", 1),
	} {
		if _, err := parseRegistry([]byte(raw)); err == nil {
			t.Fatal("invalid registry accepted", raw)
		}
	}
}
