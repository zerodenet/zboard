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
	"testing"
)

func publicMarketFixture(t *testing.T) (*Manager, *[]byte, *MarketRelease, *string) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	raw := fixtureSignedPackage(t, priv, pub, nil)
	sum := sha256.Sum256(raw)
	release := MarketRelease{Version: "v1.0.0", Artifacts: []MarketArtifact{{Platform: runtime.GOOS + "-" + runtime.GOARCH, URL: "https://github.com/example/plugin/releases/download/v1.0.0/plugin.zbplugin", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(raw))}}}
	key := base64.StdEncoding.EncodeToString(pub)
	m, _, _ := testManager(t, nil)
	m.fetch = func(_ context.Context, u string, _ int64) ([]byte, error) {
		switch u {
		case DefaultRegistryURL:
			return []byte(`{"schema_version":1,"host":"zboard","plugins":[{"id":"example.welcome","name":"Welcome","repository":"https://github.com/example/plugin","publisher":{"id":"test.publisher"},"source":{"version":"v1.0.0"}}]}`), nil
		case "https://github.com/example/plugin/releases/download/v1.0.0/marketplace-entry.json":
			return json.Marshal(map[string]any{"id": "example.welcome", "repository": "https://github.com/example/plugin", "publisher": map[string]string{"id": "test.publisher", "public_key": key}, "releases": []MarketRelease{release}})
		case "https://github.com/example/plugin/releases/download/v1.0.0/plugin.zbplugin":
			return raw, nil
		default:
			return nil, errors.New("unexpected fetch: " + u)
		}
	}
	return m, &raw, &release, &key
}

func TestPublicMarketInstallRequiresPackageInspectionAndPluginScopedTrust(t *testing.T) {
	m, _, _, _ := publicMarketFixture(t)
	ctx := context.Background()
	d, err := m.MarketDetail(ctx, "example.welcome")
	if err != nil || d.Release == nil || d.Installed != nil {
		t.Fatal(d, err)
	}
	p, err := m.PreviewMarket(ctx, "example.welcome")
	if err != nil || p.Trusted {
		t.Fatal(p, err)
	}
	var count int64
	m.db.Table("plugin_installations").Count(&count)
	if count != 0 {
		t.Fatal("inspection installed plugin")
	}
	for _, c := range []struct{ digest, key string }{{"", p.Fingerprint}, {"stale", p.Fingerprint}, {p.Digest, ""}, {p.Digest, "wrong"}} {
		if _, err := m.InstallMarketConfirmed(ctx, "example.welcome", c.digest, c.key, "admin"); err == nil {
			t.Fatal("unconfirmed package installed")
		}
	}
	installed, err := m.InstallMarketConfirmed(ctx, "example.welcome", p.Digest, p.Fingerprint, "admin")
	if err != nil || installed.Enabled {
		t.Fatal(installed, err)
	}
	d, err = m.MarketDetail(ctx, "example.welcome")
	if err != nil || d.Installed == nil || d.Installed.Digest != p.Digest {
		t.Fatal(d, err)
	}
	p, err = m.PreviewMarket(ctx, "example.welcome")
	if err != nil || !p.Trusted {
		t.Fatal("pinned trust missing", err)
	}
}
func TestPublicMarketRejectsChangedBytesAndWrongPackageIdentity(t *testing.T) {
	for _, mode := range []string{"bytes", "identity", "key"} {
		t.Run(mode, func(t *testing.T) {
			m, raw, release, key := publicMarketFixture(t)
			ctx := context.Background()
			p, err := m.PreviewMarket(ctx, "example.welcome")
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "bytes":
				*raw = append(*raw, 0)
			case "key":
				pub, _, _ := ed25519.GenerateKey(rand.Reader)
				*key = base64.StdEncoding.EncodeToString(pub)
			case "identity":
				pub, priv, _ := ed25519.GenerateKey(rand.Reader)
				*raw = fixtureSignedPackage(t, priv, pub, func(m *Manifest, _ map[string][]byte) { m.ID = "example.other" })
				*key = base64.StdEncoding.EncodeToString(pub)
				sum := sha256.Sum256(*raw)
				release.Artifacts[0].SHA256 = hex.EncodeToString(sum[:])
				release.Artifacts[0].Size = int64(len(*raw))
			}
			if _, err := m.InstallMarketConfirmed(ctx, "example.welcome", p.Digest, p.Fingerprint, "admin"); err == nil {
				t.Fatal("changed release installed")
			}
		})
	}
}
func TestPublicMarketKeepsOtherPlatformsDownloadableWithoutInstallingThem(t *testing.T) {
	m, _, release, _ := publicMarketFixture(t)
	release.Artifacts[0].Platform = "windows-arm64"
	if runtime.GOOS == "windows" && runtime.GOARCH == "arm64" {
		release.Artifacts[0].Platform = "linux-amd64"
	}
	d, err := m.MarketDetail(context.Background(), "example.welcome")
	if err != nil || d.Release == nil || d.Notice == "" {
		t.Fatal(d, err)
	}
	if _, err := m.PreviewMarket(context.Background(), "example.welcome"); err == nil {
		t.Fatal("wrong platform accepted")
	}
}
func TestPublicReleaseRejectsArtifactOutsideRepositoryAndMalformedMetadata(t *testing.T) {
	for _, url := range []string{"https://evil.example/plugin.zbplugin", "https://github.com/example/plugin/releases/download/v1.0.0/%2fsecret.zbplugin", "https://127.0.0.1/plugin.zbplugin"} {
		m, _, r, _ := publicMarketFixture(t)
		r.Artifacts[0].URL = url
		d, err := m.MarketDetail(context.Background(), "example.welcome")
		if err != nil || d.Release != nil || d.Notice == "" {
			t.Fatal(d, err)
		}
	}
}

func TestMarketInstallAllowsDevHostWithAdvisoryVersionRange(t *testing.T) {
	m, _, _, _ := publicMarketFixture(t)
	m.host = "v0.0.2-dev.202609091428"
	p, err := m.PreviewMarket(context.Background(), "example.welcome")
	if err != nil || !p.Compatibility.Compatible || p.Compatibility.Warning == "" {
		t.Fatal(p, err)
	}
	if _, err := m.InstallMarketConfirmed(context.Background(), "example.welcome", p.Digest, p.Fingerprint, "admin"); err != nil {
		t.Fatal(err)
	}
	var count int64
	m.db.Table("plugin_installations").Count(&count)
	if count != 1 {
		t.Fatal("compatible package was not installed")
	}
}
