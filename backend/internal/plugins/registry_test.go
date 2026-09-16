package plugins

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

const registryFixture = `{"schema_version":2,"host":"zboard","plugins":[{"id":"zboard.oauth","repository":"https://github.com/higanbana986/zboard-oauth","publisher":{"id":"higanbana986","public_key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},"name":"OAuth from directory","description":"Directory description","license":"MPL-2.0","maintainers":["higanbana986"],"release_source":{"type":"github-releases","metadata_asset":"marketplace-entry.json"},"surfaces":["public","account","admin"],"capabilities":["zboard.ui.page.v1","zboard.identity.provider.v1"]}]}`

func TestDefaultMarketReadsAdmissionWithoutCopyingReleaseVersions(t *testing.T) {
	m, db, _ := testManager(t, nil)
	calls := 0
	m.fetch = func(_ context.Context, url string, limit int64) ([]byte, error) {
		calls++
		if url == DefaultMarketplaceAPIURL+"/zboard/stable.json" {
			return nil, errors.New("marketplace not deployed")
		}
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
	if calls != 2 {
		t.Fatal("market did not use the compatibility fallback", calls)
	}
	var count int64
	db.Table("plugin_installations").Count(&count)
	if count != 0 {
		t.Fatal("directory lookup installed a plugin")
	}
}

func TestUnifiedMarketplacePageCarriesOnlyZBoardPackages(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	raw := `{"snapshot_version":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generated_at":"` + now + `","page":1,"page_size":1000,"total":1,"items":[{"id":"oauth.product","name":"OAuth","description":"Directory description","license":"MPL-2.0","maintainers":["higanbana986"],"repository":"https://github.com/higanbana986/zboard-oauth","publisher":{"id":"higanbana986","public_key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},"targets":[{"host":"zboard","package_id":"zboard.oauth","surfaces":["public"],"capabilities":["zboard.ui.page.v1"],"releases":[{"version":"1.0.0","channel":"stable","published_at":"` + now + `","notes_url":"https://github.com/higanbana986/zboard-oauth/releases/tag/v1.0.0","host_version":{"min":"0.0.1","max_exclusive":"0.1.0"},"surfaces":["public"],"capabilities":["zboard.ui.page.v1"],"artifacts":[{"os":"any","arch":"any","url":"https://github.com/higanbana986/zboard-oauth/releases/download/v1.0.0/oauth.zbplugin","size":42,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}]}]}]}`
	market, _, _, err := parseMarketplacePage([]byte(raw), "stable", "0.0.1", "darwin", "arm64")
	if err != nil || len(market.Entries) != 1 || market.Entries[0].ID != "zboard.oauth" || len(market.Entries[0].releases) != 1 || market.Entries[0].releases[0].Artifacts[0].Platform != "any" {
		t.Fatal(market, err)
	}
	if _, _, _, err := parseMarketplacePage([]byte(strings.Replace(raw, `"host":"zboard"`, `"host":"znet-sink"`, 1)), "stable", "0.0.1", "darwin", "arm64"); err == nil {
		t.Fatal("another host target entered the ZBoard marketplace")
	}
	incompatible, _, _, err := parseMarketplacePage([]byte(raw), "stable", "0.1.0", "darwin", "arm64")
	if err != nil || len(incompatible.Entries) != 1 || len(incompatible.Entries[0].releases) != 0 {
		t.Fatal("incompatible host version was not filtered locally", incompatible, err)
	}
}

func TestUnifiedMarketplaceRequestsAndMergesExplicitChannels(t *testing.T) {
	m, _, _ := testManager(t, nil)
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	versions := map[string]string{"stable": "1.0.0", "rc": "1.1.0-rc.1", "dev": "1.2.0-dev.1"}
	seen := map[string]bool{}
	m.fetch = func(_ context.Context, target string, limit int64) ([]byte, error) {
		if limit != 2<<20 || !strings.HasPrefix(target, DefaultMarketplaceAPIURL+"/zboard/") || !strings.HasSuffix(target, ".json") {
			return nil, errors.New("marketplace request omitted static host path")
		}
		for channel, version := range versions {
			if target == DefaultMarketplaceAPIURL+"/zboard/"+channel+".json" {
				seen[channel] = true
				return []byte(fmt.Sprintf(`{"snapshot_version":"sha256:%s","generated_at":"%s","page":1,"page_size":1000,"total":1,"items":[{"id":"zboard.oauth","name":"OAuth","description":"Directory description","license":"MPL-2.0","maintainers":["higanbana986"],"repository":"https://github.com/higanbana986/zboard-oauth","publisher":{"id":"higanbana986","public_key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},"targets":[{"host":"zboard","package_id":"zboard.oauth","surfaces":["public"],"capabilities":["zboard.ui.page.v1"],"releases":[{"version":"%s","channel":"%s","published_at":"%s","notes_url":"https://github.com/higanbana986/zboard-oauth/releases/tag/v%s","host_version":{"min":"0.0.1","max_exclusive":"0.1.0"},"surfaces":["public"],"capabilities":["zboard.ui.page.v1"],"artifacts":[{"os":"any","arch":"any","url":"https://github.com/higanbana986/zboard-oauth/releases/download/v%s/oauth.zbplugin","size":42,"sha256":"%s"}]}]}]}]}`, strings.Repeat("a", 64), now, version, channel, now, version, version, strings.Repeat("a", 64))), nil
			}
		}
		return nil, errors.New("missing channel")
	}
	market, err := m.publicMarketplaceSnapshot(context.Background())
	if err != nil || len(seen) != 3 || len(market.Entries) != 1 || len(market.Entries[0].releases) != 3 || market.Entries[0].releases[0].Version != "1.2.0-dev.1" {
		t.Fatal(market, seen, err)
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
