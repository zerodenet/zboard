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
	"runtime"
	"strings"
	"testing"
)

type publicMarketTestFixture struct {
	manager    *Manager
	packageRaw []byte
	key        string
	releases   map[string]MarketRelease
}

func newPublicMarketFixture(t *testing.T) *publicMarketTestFixture {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	packageRaw := fixtureSignedPackage(t, priv, pub, nil)
	sum := sha256.Sum256(packageRaw)
	key := base64.StdEncoding.EncodeToString(pub)
	versions := []string{"1.2.0-dev.4", "1.1.0-rc.2", "1.0.0"}
	releases := map[string]MarketRelease{}
	for _, version := range versions {
		releases[version] = MarketRelease{
			Version: version,
			Artifacts: []MarketArtifact{{
				Platform: runtime.GOOS + "-" + runtime.GOARCH,
				URL:      "https://github.com/example/plugin/releases/download/v" + version + "/plugin.zbplugin",
				SHA256:   hex.EncodeToString(sum[:]), Size: int64(len(packageRaw)),
			}},
		}
	}
	manager, _, _ := testManager(t, nil)
	fixture := &publicMarketTestFixture{manager: manager, packageRaw: packageRaw, key: key, releases: releases}
	manager.fetch = fixture.fetch
	return fixture
}

func (fixture *publicMarketTestFixture) registry() []byte {
	return []byte(`{"schema_version":2,"host":"zboard","plugins":[{"id":"example.welcome","repository":"https://github.com/example/plugin","publisher":{"id":"test.publisher","public_key":"` + fixture.key + `"},"name":"Welcome from directory","description":"Directory metadata","license":"MPL-2.0","maintainers":["example"],"release_source":{"type":"github-releases","metadata_asset":"marketplace-entry.json"},"surfaces":["public","account","admin"],"capabilities":["zboard.ui.page.v1","zboard.config.v1"]}]}`)
}

func (fixture *publicMarketTestFixture) releaseDocument(version string) []byte {
	release := fixture.releases[version]
	raw, _ := json.Marshal(map[string]any{
		"id": "example.welcome", "repository": "https://github.com/example/plugin",
		"publisher": map[string]string{"id": "test.publisher", "public_key": fixture.key},
		"releases": []map[string]any{{
			"version":      "v" + version,
			"surfaces":     []string{"public", "account", "admin"},
			"capabilities": []string{"zboard.ui.page.v1", "zboard.config.v1"},
			"artifacts":    release.Artifacts,
		}},
	})
	return raw
}

func (fixture *publicMarketTestFixture) fetch(_ context.Context, target string, _ int64) ([]byte, error) {
	switch target {
	case DefaultRegistryURL:
		return fixture.registry(), nil
	case "https://api.github.com/repos/example/plugin/releases":
		return []byte(`[
			{"tag_name":"v1.2.0-dev.4","name":"Development release","body":"Dev notes","html_url":"https://github.com/example/plugin/releases/tag/v1.2.0-dev.4","published_at":"2026-09-11T04:08:00Z","draft":false,"prerelease":true,"assets":[{"name":"marketplace-entry.json","browser_download_url":"https://github.com/example/plugin/releases/download/v1.2.0-dev.4/marketplace-entry.json"}]},
			{"tag_name":"v1.1.0-rc.2","name":"Release candidate","body":"RC notes","html_url":"https://github.com/example/plugin/releases/tag/v1.1.0-rc.2","published_at":"2026-09-10T04:08:00Z","draft":false,"prerelease":true,"assets":[{"name":"marketplace-entry.json","browser_download_url":"https://github.com/example/plugin/releases/download/v1.1.0-rc.2/marketplace-entry.json"}]},
			{"tag_name":"v1.0.0","name":"Latest stable","body":"Stable release notes","html_url":"https://github.com/example/plugin/releases/tag/v1.0.0","published_at":"2026-09-09T04:08:00Z","draft":false,"prerelease":false,"assets":[{"name":"marketplace-entry.json","browser_download_url":"https://github.com/example/plugin/releases/download/v1.0.0/marketplace-entry.json"}]},
			{"tag_name":"v1.3.0-beta.1","name":"Beta","body":"Unsupported","html_url":"https://github.com/example/plugin/releases/tag/v1.3.0-beta.1","published_at":"2026-09-12T04:08:00Z","draft":false,"prerelease":true,"assets":[{"name":"marketplace-entry.json","browser_download_url":"https://github.com/example/plugin/releases/download/v1.3.0-beta.1/marketplace-entry.json"}]}
		]`), nil
	case "https://github.com/example/plugin/releases/download/v1.0.0/marketplace-entry.json":
		return fixture.releaseDocument("1.0.0"), nil
	case "https://github.com/example/plugin/releases/download/v1.1.0-rc.2/marketplace-entry.json":
		return fixture.releaseDocument("1.1.0-rc.2"), nil
	case "https://github.com/example/plugin/releases/download/v1.2.0-dev.4/marketplace-entry.json":
		return fixture.releaseDocument("1.2.0-dev.4"), nil
	case "https://github.com/example/plugin/releases/download/v1.0.0/plugin.zbplugin":
		return fixture.packageRaw, nil
	default:
		return nil, errors.New("unexpected fetch: " + target)
	}
}

func TestPublicMarketDiscoversPublisherOwnedStableRCAndDev(t *testing.T) {
	fixture := newPublicMarketFixture(t)
	detail, err := fixture.manager.MarketDetail(context.Background(), "example.welcome", "")
	if err != nil || detail.Entry.Name != "Welcome from directory" || detail.Release == nil || detail.Release.Version != "1.0.0" || detail.Release.Channel != "stable" ||
		detail.Release.Title != "Latest stable" || detail.Release.Notes != "Stable release notes" ||
		len(detail.Releases) != 3 || detail.Releases[0].Version != "1.2.0-dev.4" {
		t.Fatal(detail, err)
	}
	rc, err := fixture.manager.MarketDetail(context.Background(), "example.welcome", "1.1.0-rc.2")
	if err != nil || rc.Release == nil || rc.Release.Channel != "rc" || len(rc.Release.Artifacts) != 1 {
		t.Fatal(rc, err)
	}
	if _, err := fixture.manager.MarketDetail(context.Background(), "example.welcome", "1.3.0-beta.1"); err == nil {
		t.Fatal("unsupported publisher release was selectable")
	}
}

func TestPublicMarketUsesAdmittedPublisherTrustAndInstallsSelectedVersion(t *testing.T) {
	fixture := newPublicMarketFixture(t)
	preview, err := fixture.manager.PreviewMarket(context.Background(), "example.welcome", "1.0.0")
	if err != nil || !preview.Trusted {
		t.Fatal(preview, err)
	}
	for _, digest := range []string{"", "stale"} {
		if _, err := fixture.manager.InstallMarketConfirmed(context.Background(), "example.welcome", "1.0.0", digest, "", "admin"); err == nil {
			t.Fatal("stale confirmation installed package")
		}
	}
	installed, err := fixture.manager.InstallMarketConfirmed(context.Background(), "example.welcome", "1.0.0", preview.Digest, "", "admin")
	if err != nil || installed.Enabled || installed.Version != "1.0.0" {
		t.Fatal(installed, err)
	}
}

func TestPublicMarketRejectsChangedPackageBytes(t *testing.T) {
	fixture := newPublicMarketFixture(t)
	preview, err := fixture.manager.PreviewMarket(context.Background(), "example.welcome", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	fixture.packageRaw = append(fixture.packageRaw, 0)
	if _, err := fixture.manager.InstallMarketConfirmed(context.Background(), "example.welcome", "1.0.0", preview.Digest, "", "admin"); err == nil {
		t.Fatal("changed package installed")
	}
}

func TestPublicMarketRejectsReleaseOutsideAdmissionBoundary(t *testing.T) {
	for _, change := range []func(*publicMarketTestFixture){
		func(f *publicMarketTestFixture) {
			release := f.releases["1.0.0"]
			release.Artifacts[0].URL = "https://evil.example/plugin.zbplugin"
			f.releases["1.0.0"] = release
		},
	} {
		fixture := newPublicMarketFixture(t)
		change(fixture)
		detail, err := fixture.manager.MarketDetail(context.Background(), "example.welcome", "1.0.0")
		if err != nil || detail.Notice == "" || detail.Release == nil || len(detail.Release.Artifacts) != 0 {
			t.Fatal(detail, err)
		}
	}
	fixture := newPublicMarketFixture(t)
	baseFetch := fixture.manager.fetch
	fixture.manager.fetch = func(ctx context.Context, target string, limit int64) ([]byte, error) {
		raw, err := baseFetch(ctx, target, limit)
		if strings.HasSuffix(target, "/marketplace-entry.json") && err == nil {
			return []byte(strings.Replace(string(raw), `"zboard.config.v1"`, `"zboard.storage.v1"`, 1)), nil
		}
		return raw, err
	}
	if _, err := fixture.manager.PreviewMarket(context.Background(), "example.welcome", "1.0.0"); err == nil {
		t.Fatal("release outside capability ceiling was accepted")
	}
	fixture = newPublicMarketFixture(t)
	baseFetch = fixture.manager.fetch
	fixture.manager.fetch = func(ctx context.Context, target string, limit int64) ([]byte, error) {
		raw, err := baseFetch(ctx, target, limit)
		if strings.HasSuffix(target, "/marketplace-entry.json") && err == nil {
			otherKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
			return []byte(strings.Replace(string(raw), fixture.key, otherKey, 1)), nil
		}
		return raw, err
	}
	if _, err := fixture.manager.PreviewMarket(context.Background(), "example.welcome", "1.0.0"); err == nil {
		t.Fatal("release changed the admitted publisher key")
	}
}

func TestPublicMarketKeepsOtherPlatformsDownloadableWithoutInstallingThem(t *testing.T) {
	fixture := newPublicMarketFixture(t)
	release := fixture.releases["1.0.0"]
	release.Artifacts[0].Platform = "windows-arm64"
	if runtime.GOOS == "windows" && runtime.GOARCH == "arm64" {
		release.Artifacts[0].Platform = "linux-amd64"
	}
	fixture.releases["1.0.0"] = release
	detail, err := fixture.manager.MarketDetail(context.Background(), "example.welcome", "1.0.0")
	if err != nil || detail.Release == nil || detail.Notice == "" {
		t.Fatal(detail, err)
	}
	if _, err := fixture.manager.PreviewMarket(context.Background(), "example.welcome", "1.0.0"); err == nil {
		t.Fatal("wrong-platform package accepted")
	}
}

func TestMarketInstallAllowsDevHostWithAdvisoryVersionRange(t *testing.T) {
	fixture := newPublicMarketFixture(t)
	fixture.manager.host = "v0.0.2-dev.202609091428"
	preview, err := fixture.manager.PreviewMarket(context.Background(), "example.welcome", "1.0.0")
	if err != nil || !preview.Compatibility.Compatible || preview.Compatibility.Warning == "" {
		t.Fatal(preview, err)
	}
}
