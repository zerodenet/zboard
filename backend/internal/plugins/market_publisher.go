package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

func (m *Manager) discoverPublisherReleases(ctx context.Context, entry MarketEntry) ([]MarketRelease, error) {
	repository := strings.TrimPrefix(strings.TrimRight(entry.Repository, "/"), "https://github.com/")
	if repository == entry.Repository || strings.Count(repository, "/") != 1 ||
		entry.ReleaseSource.Type != "github-releases" || !safeMetadataAsset(entry.ReleaseSource.MetadataAsset) {
		return nil, errors.New("unsupported plugin release source")
	}
	cacheKey := entry.Repository + "\n" + entry.ReleaseSource.MetadataAsset
	m.marketMu.Lock()
	cached, ok := m.marketReleases[cacheKey]
	m.marketMu.Unlock()
	if ok && cached.expiresAt.After(time.Now()) {
		return append([]MarketRelease(nil), cached.releases...), nil
	}
	raw, err := m.fetch(ctx, "https://api.github.com/repos/"+repository+"/releases", 2<<20)
	if err != nil {
		return nil, err
	}
	var published []struct {
		TagName     string    `json:"tag_name"`
		Name        string    `json:"name"`
		Body        string    `json:"body"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
		Draft       bool      `json:"draft"`
		Prerelease  bool      `json:"prerelease"`
		Assets      []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(raw, &published); err != nil || len(published) > 100 {
		return nil, errors.New("invalid publisher release response")
	}
	seen := map[string]bool{}
	releases := make([]MarketRelease, 0, len(published))
	for _, candidate := range published {
		if candidate.Draft || !strings.HasPrefix(candidate.TagName, "v") {
			continue
		}
		version := strings.TrimPrefix(candidate.TagName, "v")
		channel, channelErr := marketReleaseChannel(version)
		if channelErr != nil || candidate.Prerelease != (channel != "stable") {
			continue
		}
		metadata := ""
		for _, asset := range candidate.Assets {
			if asset.Name != entry.ReleaseSource.MetadataAsset {
				continue
			}
			expected := strings.TrimRight(entry.Repository, "/") + "/releases/download/" +
				candidate.TagName + "/" + entry.ReleaseSource.MetadataAsset
			if metadata != "" || asset.BrowserDownloadURL != expected {
				metadata = ""
				break
			}
			metadata = asset.BrowserDownloadURL
		}
		if metadata == "" {
			continue
		}
		expectedReleaseURL := strings.TrimRight(entry.Repository, "/") + "/releases/tag/" + candidate.TagName
		if candidate.HTMLURL != expectedReleaseURL || candidate.PublishedAt.IsZero() {
			continue
		}
		if seen[version] {
			return nil, errors.New("duplicate publisher release version")
		}
		seen[version] = true
		releases = append(releases, MarketRelease{
			Version: version, Channel: channel, Title: boundedMarketText(candidate.Name, candidate.TagName, 200),
			Notes: boundedMarketText(candidate.Body, "", 20_000), URL: candidate.HTMLURL,
			PublishedAt: candidate.PublishedAt, Artifacts: []MarketArtifact{}, metadata: metadata,
		})
	}
	sort.Slice(releases, func(i, j int) bool {
		left, _ := semver.StrictNewVersion(releases[i].Version)
		right, _ := semver.StrictNewVersion(releases[j].Version)
		return left.GreaterThan(right)
	})
	if len(releases) == 0 {
		return nil, errors.New("publisher has no installable releases")
	}
	m.marketMu.Lock()
	m.marketReleases[cacheKey] = marketReleaseCache{expiresAt: time.Now().Add(5 * time.Minute), releases: append([]MarketRelease(nil), releases...)}
	m.marketMu.Unlock()
	return releases, nil
}

func boundedMarketText(value, fallback string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	runes := []rune(value)
	if len(runes) > limit {
		value = string(runes[:limit])
	}
	return value
}
