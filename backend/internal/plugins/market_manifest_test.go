package plugins

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func unifiedManifestFixture(t *testing.T, fixture *publicMarketTestFixture) map[string]any {
	t.Helper()
	a := fixture.releases["1.0.0"].Artifacts[0]
	platform := strings.Split(a.Platform, "-")
	return map[string]any{
		"schema_version": 1, "product_id": "welcome.product", "repository": "https://github.com/example/plugin",
		"publisher": map[string]string{"id": "test.publisher", "public_key": fixture.key},
		"source":    map[string]string{"tag": "v1.0.0", "commit": strings.Repeat("a", 40)},
		"release": map[string]any{"version": "1.0.0", "channel": "stable", "published_at": time.Now().UTC().Format(time.RFC3339), "notes_url": "https://github.com/example/plugin/releases/tag/v1.0.0", "targets": []any{
			map[string]any{"host": "znet-sink", "package_id": "welcome.sink", "capabilities": []string{"network.request"}},
			map[string]any{"host": "zboard", "package_id": "example.welcome", "host_version": map[string]string{"min": "0.0.1", "max_exclusive": "0.1.0"}, "surfaces": []string{"public", "account", "admin"}, "capabilities": []string{"zboard.ui.page.v1", "zboard.config.v1"}, "artifacts": []any{map[string]any{"os": platform[0], "arch": platform[1], "url": a.URL, "sha256": a.SHA256, "size": a.Size}}},
		}},
	}
}

func TestMarketplaceCompatibilityFallbackAcceptsUnifiedPublisherManifest(t *testing.T) {
	fixture := newPublicMarketFixture(t)
	doc := unifiedManifestFixture(t, fixture)
	raw, _ := json.Marshal(doc)
	fixture.manager.fetch = func(ctx context.Context, target string, limit int64) ([]byte, error) {
		if target == "https://github.com/example/plugin/releases/download/v1.0.0/marketplace-entry.json" {
			return raw, nil
		}
		return fixture.fetch(ctx, target, limit)
	}
	preview, err := fixture.manager.PreviewMarket(context.Background(), "example.welcome", "1.0.0")
	if err != nil || preview.Manifest.ID != "example.welcome" || !preview.Trusted {
		t.Fatal(preview, err)
	}
	installed, err := fixture.manager.InstallMarketConfirmed(context.Background(), "example.welcome", "1.0.0", preview.Digest, "", "admin")
	if err != nil || installed.ID != "example.welcome" || installed.Enabled {
		t.Fatal(installed, err)
	}
}

func TestUnifiedPublisherManifestRejectsWrongHostIdentityVersionAndBoundary(t *testing.T) {
	fixture := newPublicMarketFixture(t)
	market, err := parseRegistry(fixture.registry())
	if err != nil {
		t.Fatal(err)
	}
	entry := market.Entries[0]
	for _, kind := range []string{"package", "publisher", "version", "source", "channel", "capabilities", "range", "duplicate-host"} {
		t.Run(kind, func(t *testing.T) {
			doc := unifiedManifestFixture(t, fixture)
			release := doc["release"].(map[string]any)
			targets := release["targets"].([]any)
			target := targets[1].(map[string]any)
			switch kind {
			case "package":
				target["package_id"] = "welcome.sink"
			case "publisher":
				doc["publisher"] = map[string]string{"id": "other", "public_key": fixture.key}
			case "version":
				release["version"] = "1.0.1"
			case "source":
				doc["source"] = map[string]string{"tag": "v1.0.0", "commit": "main"}
			case "channel":
				release["channel"] = "dev"
			case "capabilities":
				target["capabilities"] = []string{"network.request"}
			case "range":
				target["host_version"] = map[string]string{"min": "0.1.0"}
			case "duplicate-host":
				targets[0] = target
			}
			raw, _ := json.Marshal(doc)
			if _, err := parsePublisherRelease(raw, entry, "1.0.0", "0.0.1"); err == nil {
				t.Fatal("invalid new manifest accepted")
			}
		})
	}
}

func TestUnifiedSnapshotInstallationRechecksSelectedPackageAndWithdrawal(t *testing.T) {
	fixture := newPublicMarketFixture(t)
	doc := unifiedManifestFixture(t, fixture)
	release := doc["release"].(map[string]any)
	target := release["targets"].([]any)[1].(map[string]any)
	for _, key := range []string{"version", "channel", "published_at", "notes_url"} {
		target[key] = release[key]
	}
	hostTarget := map[string]any{"host": "zboard", "package_id": "example.welcome", "surfaces": target["surfaces"], "capabilities": target["capabilities"], "releases": []any{target}}
	item := map[string]any{"id": "welcome.product", "name": "Welcome", "description": "Signed fixture", "license": "MIT", "maintainers": []string{"example"}, "repository": doc["repository"], "publisher": doc["publisher"], "targets": []any{hostTarget}}
	withdrawn := false
	fixture.manager.fetch = func(ctx context.Context, url string, limit int64) ([]byte, error) {
		if strings.HasPrefix(url, DefaultMarketplaceAPIURL+"/zboard/") {
			items := []any{}
			if strings.HasSuffix(url, "/stable.json") && !withdrawn {
				items = append(items, item)
			}
			return json.Marshal(map[string]any{"snapshot_version": "sha256:" + strings.Repeat("a", 64), "generated_at": "2026-09-15T11:09:04Z", "page": 1, "page_size": 1000, "total": len(items), "items": items})
		}
		if url == fixture.releases["1.0.0"].Artifacts[0].URL {
			return fixture.packageRaw, nil
		}
		t.Fatalf("snapshot installation fetched unrelated metadata: %s", url)
		return nil, nil
	}
	preview, err := fixture.manager.PreviewMarket(context.Background(), "example.welcome", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.InstallMarketConfirmed(context.Background(), "example.welcome", "1.0.0", "stale", "", "admin"); err == nil {
		t.Fatal("stale digest accepted")
	}
	withdrawn = true
	if _, err := fixture.manager.InstallMarketConfirmed(context.Background(), "example.welcome", "1.0.0", preview.Digest, "", "admin"); err == nil {
		t.Fatal("withdrawn package installed")
	}
	withdrawn = false
	installed, err := fixture.manager.InstallMarketConfirmed(context.Background(), "example.welcome", "1.0.0", preview.Digest, "", "admin")
	if err != nil || installed.Enabled || installed.ID != "example.welcome" {
		t.Fatal(installed, err)
	}
	// The admission ceiling is larger than this release's declarations. The signed
	// package must still fit the selected release, not just the product ceiling.
	target["capabilities"] = []string{"zboard.ui.page.v1"}
	if _, err := fixture.manager.PreviewMarket(context.Background(), "example.welcome", "1.0.0"); err == nil {
		t.Fatal("signed package exceeded release declaration")
	}
}
