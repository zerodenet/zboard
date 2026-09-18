package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func snapshotFixture(t *testing.T, channel string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/marketplace/" + channel + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPublishedMarketplaceSnapshotPreservesProductIdentityAndDownloads(t *testing.T) {
	market, _, _, err := parseMarketplacePage(snapshotFixture(t, "stable"), "stable", "0.0.2", "darwin", "arm64")
	if err != nil || len(market.Entries) != 2 {
		t.Fatal(market, err)
	}
	connect := market.Entries[0]
	if connect.ID != "org.zerodenet.connect.zboard" || connect.ProductID != "org.zerodenet.connect" || len(connect.releases) != 1 {
		t.Fatal(connect)
	}
	release := connect.releases[0]
	if release.Title != "Connect 0.0.1" || release.Notes == "" || len(release.Artifacts) != 1 || release.Artifacts[0].Platform != "linux-amd64" {
		t.Fatal(release)
	}
	if _, err := (MarketDetail{Release: &release, Platform: "darwin-arm64"}).hostArtifact(); err == nil {
		t.Fatal("another host platform was installable")
	}
	if _, err := (MarketDetail{Release: &release, Platform: "linux-amd64"}).hostArtifact(); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceKeepsIncompatibleListingsWithoutRediscoveringReleases(t *testing.T) {
	for _, host := range []string{"0.0.1", "0.1.0-rc.1", "0.1.0", "dev"} {
		t.Run(host, func(t *testing.T) {
			m, _, _ := testManager(t, nil)
			m.host = host
			m.fetch = func(_ context.Context, target string, _ int64) ([]byte, error) {
				for _, ch := range []string{"stable", "rc", "dev"} {
					if target == DefaultMarketplaceAPIURL+"/zboard/"+ch+".json" {
						return snapshotFixture(t, ch), nil
					}
				}
				t.Fatalf("valid snapshot must not fall back to publisher discovery: %s", target)
				return nil, errors.New("unexpected request")
			}
			detail, err := m.MarketDetail(context.Background(), "org.zerodenet.connect.zboard", "")
			if err != nil || detail.Release != nil || detail.Notice == "" || detail.Entry.Name != "Connect" {
				t.Fatal(detail, err)
			}
		})
	}
}

func TestMarketplacePrereleaseHostUsesItsProductReleaseLine(t *testing.T) {
	for _, test := range []struct {
		host       string
		compatible bool
	}{
		{host: "0.0.1", compatible: false},
		{host: "0.0.2", compatible: true},
		{host: "v0.0.2-rc.1", compatible: true},
		{host: "v0.0.2-dev.202609180411", compatible: true},
		{host: "v0.1.0-rc.1", compatible: false},
		{host: "0.1.0", compatible: false},
		{host: "dev", compatible: false},
	} {
		t.Run(test.host, func(t *testing.T) {
			compatible, err := marketplaceHostCompatible("0.0.2", "0.1.0", test.host)
			if err != nil || compatible != test.compatible {
				t.Fatalf("compatible=%v, want %v: %v", compatible, test.compatible, err)
			}
		})
	}
}

func TestMarketplaceRCExposesConnectAndOAuthChannels(t *testing.T) {
	m, _, _ := testManager(t, nil)
	m.host = "v0.0.2-rc.202609180411"
	m.fetch = func(_ context.Context, target string, _ int64) ([]byte, error) {
		for _, channel := range []string{"stable", "rc", "dev"} {
			if target == DefaultMarketplaceAPIURL+"/zboard/"+channel+".json" {
				return snapshotFixture(t, channel), nil
			}
		}
		t.Fatalf("unexpected request %s", target)
		return nil, nil
	}
	market, err := m.publicMarketplaceSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]bool{
		"org.zerodenet.connect.zboard": {"stable": true, "dev": true},
		"zboard.oauth":                 {"stable": true, "dev": true},
	}
	for _, entry := range market.Entries {
		channels, ok := want[entry.ID]
		if !ok {
			continue
		}
		for _, release := range entry.releases {
			delete(channels, release.Channel)
		}
	}
	for id, channels := range want {
		if len(channels) != 0 {
			t.Fatalf("%s is missing channels: %v", id, channels)
		}
	}
}

func TestMarketplaceRejectsMixedSnapshotsAndDoesNotResurrectEmptyResults(t *testing.T) {
	m, _, _ := testManager(t, nil)
	m.fetch = func(_ context.Context, target string, _ int64) ([]byte, error) {
		for _, ch := range []string{"stable", "rc", "dev"} {
			if target == DefaultMarketplaceAPIURL+"/zboard/"+ch+".json" {
				raw := snapshotFixture(t, ch)
				if ch == "dev" {
					raw = []byte(strings.Replace(string(raw), "237b87ef", "aaaaaaaa", 1))
				}
				return raw, nil
			}
		}
		t.Fatalf("unexpected request %s", target)
		return nil, nil
	}
	if _, err := m.publicMarketplaceSnapshot(context.Background()); err == nil {
		t.Fatal("mixed generation accepted")
	}
	m.fetch = func(_ context.Context, target string, _ int64) ([]byte, error) {
		if !strings.HasPrefix(target, DefaultMarketplaceAPIURL+"/zboard/") {
			t.Fatalf("empty market caused fallback %s", target)
		}
		return snapshotFixture(t, "rc"), nil
	}
	market, err := m.Market(context.Background())
	if err != nil || len(market.Entries) != 0 || market.SnapshotVersion == "" {
		t.Fatal(market, err)
	}
}

func TestMarketplaceRejectsInvalidArtifactPathsAndCapabilityEscalation(t *testing.T) {
	for _, bad := range []string{"../evil.zbplugin", "%2e%2e%2fevil.zbplugin", "evil%5cname.zbplugin", "evil%00.zbplugin", "https://elsewhere.example/evil.zbplugin"} {
		t.Run(bad, func(t *testing.T) {
			raw := string(snapshotFixture(t, "stable"))
			raw = strings.Replace(raw, "org.zerodenet.connect.zboard-0.0.1-linux-amd64.zbplugin", bad, 1)
			if _, _, _, err := parseMarketplacePage([]byte(raw), "stable", "0.0.2", "linux", "amd64"); err == nil {
				t.Fatal("unsafe artifact accepted")
			}
		})
	}
	raw := snapshotFixture(t, "stable")
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	item := doc["items"].([]any)[0].(map[string]any)
	target := item["targets"].([]any)[0].(map[string]any)
	target["capabilities"] = []string{}
	raw, _ = json.Marshal(doc)
	if _, _, _, err := parseMarketplacePage(raw, "stable", "0.0.2", "linux", "amd64"); err == nil {
		t.Fatal("empty admitted boundary was bypassed")
	}
}

func TestMarketplaceMetadataCannotExtendHostPackageSizeLimit(t *testing.T) {
	release := snapshotRelease{Version: "1.0.0", Channel: "stable", PublishedAt: time.Now().UTC().Format(time.RFC3339), NotesURL: "https://github.com/example/plugin/releases/tag/v1.0.0", Artifacts: []snapshotArtifact{{OS: "any", Arch: "any", URL: "https://github.com/example/plugin/releases/download/v1.0.0/plugin.zbplugin", Size: MaxPackageBytes + 1, SHA256: strings.Repeat("a", 64)}}}
	release.HostVersion.Min = "0.0.1"
	converted, err := convertMarketplaceRelease(MarketEntry{Repository: "https://github.com/example/plugin"}, release, "stable", "0.0.2")
	if err != nil || converted == nil || len(converted.Artifacts) != 1 {
		t.Fatal(converted, err)
	}
	if _, err := (MarketDetail{Release: converted, Platform: "linux-amd64"}).hostArtifact(); err == nil {
		t.Fatal("oversized marketplace package became installable")
	}
}

func TestMarketplaceRejectsProductPackageChangesAcrossChannels(t *testing.T) {
	m, _, _ := testManager(t, nil)
	m.host = "0.0.2"
	m.fetch = func(_ context.Context, target string, _ int64) ([]byte, error) {
		for _, ch := range []string{"stable", "rc", "dev"} {
			if target == DefaultMarketplaceAPIURL+"/zboard/"+ch+".json" {
				raw := snapshotFixture(t, ch)
				if ch == "dev" {
					raw = []byte(strings.Replace(string(raw), `"package_id": "org.zerodenet.connect.zboard"`, `"package_id": "org.zerodenet.other.zboard"`, 1))
				}
				return raw, nil
			}
		}
		t.Fatalf("unexpected request %s", target)
		return nil, nil
	}
	if _, err := m.publicMarketplaceSnapshot(context.Background()); err == nil {
		t.Fatal("product changed package identity across channels")
	}
}

func TestMarketplaceAnyOSArtifactStillRestrictsArchitecture(t *testing.T) {
	release := MarketRelease{Artifacts: []MarketArtifact{{Platform: "any-amd64", Size: 42}}}
	if _, err := (MarketDetail{Release: &release, Platform: "linux-amd64"}).hostArtifact(); err != nil {
		t.Fatal(err)
	}
	if _, err := (MarketDetail{Release: &release, Platform: "darwin-arm64"}).hostArtifact(); err == nil {
		t.Fatal("architecture boundary bypassed")
	}
	if !validSnapshotPlatform("any", "amd64") || !validSnapshotPlatform("android", "arm64") || validSnapshotPlatform("linux", "any") {
		t.Fatal("snapshot platform validation differs from contract")
	}
}
