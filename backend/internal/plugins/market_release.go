package plugins

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"gorm.io/gorm"
)

type MarketArtifact struct {
	Platform string `json:"platform"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}
type MarketRelease struct {
	Version     string           `json:"version"`
	Channel     string           `json:"channel"`
	Title       string           `json:"title,omitempty"`
	Notes       string           `json:"notes,omitempty"`
	URL         string           `json:"url,omitempty"`
	PublishedAt time.Time        `json:"published_at,omitempty"`
	Artifacts   []MarketArtifact `json:"artifacts"`
	metadata    string
}
type marketReleaseCache struct {
	expiresAt time.Time
	releases  []MarketRelease
}
type MarketInstalled struct {
	Version string `json:"version"`
	Digest  string `json:"digest"`
	State   string `json:"state"`
}
type MarketDetail struct {
	Entry     MarketEntry      `json:"entry"`
	Platform  string           `json:"platform"`
	Release   *MarketRelease   `json:"release,omitempty"`
	Releases  []MarketRelease  `json:"releases,omitempty"`
	Installed *MarketInstalled `json:"installed,omitempty"`
	Notice    string           `json:"notice,omitempty"`
	publicKey string
	trusted   bool
}

func (m *Manager) MarketDetail(ctx context.Context, id, requestedVersion string) (MarketDetail, error) {
	market, err := m.Market(ctx)
	if err != nil {
		return MarketDetail{}, err
	}
	var entry *MarketEntry
	for i := range market.Entries {
		if market.Entries[i].ID == id {
			entry = &market.Entries[i]
			break
		}
	}
	if entry == nil {
		return MarketDetail{}, gorm.ErrRecordNotFound
	}
	detail := MarketDetail{
		Entry: *entry, Platform: runtime.GOOS + "-" + runtime.GOARCH,
		publicKey: entry.PublicKey, trusted: market.Kind == "registry" || market.Kind == "signed",
	}
	m.mu.Lock()
	installed, loadErr := m.load(id)
	m.mu.Unlock()
	if loadErr == nil {
		detail.Installed = &MarketInstalled{Version: installed.Version, Digest: installed.Digest, State: installed.State}
	} else if !errors.Is(loadErr, gorm.ErrRecordNotFound) {
		return MarketDetail{}, loadErr
	}
	if market.Kind == "signed" {
		if detail.publicKey == "" {
			detail.publicKey = m.options.TrustedPublishers[entry.Publisher]
		}
		channel, channelErr := marketReleaseChannel(entry.Version)
		if channelErr != nil {
			return MarketDetail{}, channelErr
		}
		detail.Releases = []MarketRelease{{
			Version: entry.Version, Channel: channel,
			Artifacts: []MarketArtifact{{Platform: detail.Platform, URL: entry.PackageURL, SHA256: entry.SHA256}},
		}}
		detail.Release, err = selectMarketRelease(detail.Releases, requestedMarketVersion(entry.Version, requestedVersion))
		return detail, err
	}

	detail.Releases, err = m.discoverPublisherReleases(ctx, *entry)
	if err != nil {
		detail.Notice = "无法读取开发者仓库的发行版本，请稍后刷新或前往插件仓库查看。"
		return detail, nil
	}
	detail.Release, err = selectMarketRelease(detail.Releases, requestedVersion)
	if err != nil {
		return MarketDetail{}, err
	}
	raw, fetchErr := m.fetch(ctx, detail.Release.metadata, 256<<10)
	if fetchErr == nil {
		summary := *detail.Release
		var parsed *MarketRelease
		parsed, fetchErr = parsePublisherRelease(raw, *entry, detail.Release.Version)
		if fetchErr == nil {
			parsed.Title = summary.Title
			parsed.Notes = summary.Notes
			parsed.URL = summary.URL
			parsed.PublishedAt = summary.PublishedAt
			detail.Release = parsed
		}
	}
	if fetchErr != nil {
		detail.Notice = "无法读取所选版本的发行信息，请稍后刷新或前往插件仓库查看。"
		return detail, nil
	}
	for i := range detail.Releases {
		if detail.Releases[i].Version == detail.Release.Version {
			detail.Releases[i] = *detail.Release
			break
		}
	}
	if _, artifactErr := detail.hostArtifact(); artifactErr != nil {
		detail.Notice = "此版本没有适用于当前服务器的安装包，可以下载其他平台的安装包。"
	}
	return detail, nil
}

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

func requestedMarketVersion(fallback, requested string) string {
	if strings.TrimSpace(requested) == "" {
		return strings.TrimPrefix(fallback, "v")
	}
	return strings.TrimPrefix(strings.TrimSpace(requested), "v")
}

func marketReleaseChannel(version string) (string, error) {
	value, err := semver.StrictNewVersion(strings.TrimPrefix(version, "v"))
	if err != nil {
		return "", errors.New("invalid market release version")
	}
	pre := strings.ToLower(value.Prerelease())
	switch {
	case pre == "":
		return "stable", nil
	case marketPrereleaseMatches(pre, "rc"):
		return "rc", nil
	case marketPrereleaseMatches(pre, "dev"):
		return "dev", nil
	default:
		return "", errors.New("unsupported market release channel")
	}
}

func marketPrereleaseMatches(prerelease, channel string) bool {
	if prerelease == channel || strings.HasPrefix(prerelease, channel+".") {
		return true
	}
	suffix := strings.TrimPrefix(prerelease, channel)
	return suffix != prerelease && suffix != "" && suffix[0] >= '0' && suffix[0] <= '9'
}

func selectMarketRelease(releases []MarketRelease, requested string) (*MarketRelease, error) {
	version := strings.TrimPrefix(strings.TrimSpace(requested), "v")
	if version == "" {
		for _, channel := range []string{"stable", "rc", "dev"} {
			for i := range releases {
				if releases[i].Channel == channel {
					selected := releases[i]
					return &selected, nil
				}
			}
		}
		return nil, errors.New("plugin has no selectable release")
	}
	if _, err := marketReleaseChannel(version); err != nil {
		return nil, err
	}
	for i := range releases {
		if releases[i].Version == version {
			selected := releases[i]
			return &selected, nil
		}
	}
	return nil, errors.New("selected plugin version is not published")
}

func parsePublisherRelease(raw []byte, entry MarketEntry, selectedVersion string) (*MarketRelease, error) {
	var document struct {
		ID         string `json:"id"`
		Repository string `json:"repository"`
		Publisher  struct {
			ID        string `json:"id"`
			PublicKey string `json:"public_key"`
		} `json:"publisher"`
		Releases []struct {
			Version      string           `json:"version"`
			Surfaces     []string         `json:"surfaces"`
			Capabilities []string         `json:"capabilities"`
			Artifacts    []MarketArtifact `json:"artifacts"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	if document.ID != entry.ID || strings.TrimRight(document.Repository, "/") != strings.TrimRight(entry.Repository, "/") ||
		document.Publisher.ID != entry.Publisher || document.Publisher.PublicKey != entry.PublicKey ||
		len(document.Releases) != 1 {
		return nil, errors.New("release identity differs from marketplace admission")
	}
	key, err := base64.StdEncoding.DecodeString(document.Publisher.PublicKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("invalid admitted publisher key")
	}
	published := document.Releases[0]
	version := strings.TrimPrefix(published.Version, "v")
	channel, err := marketReleaseChannel(version)
	if err != nil || version != strings.TrimPrefix(selectedVersion, "v") {
		return nil, errors.New("release metadata version mismatch")
	}
	if !withinListingBoundary(published.Surfaces, entry.Surfaces) ||
		!withinListingBoundary(published.Capabilities, entry.Capabilities) {
		return nil, errors.New("release exceeds marketplace capability boundary")
	}
	if len(published.Artifacts) == 0 || len(published.Artifacts) > 20 {
		return nil, errors.New("release has no packages")
	}
	prefix := strings.TrimRight(entry.Repository, "/") + "/releases/download/v" + version + "/"
	seen := map[string]bool{}
	for _, artifact := range published.Artifacts {
		u, urlErr := safeRemoteURL(artifact.URL)
		sum, hashErr := hex.DecodeString(artifact.SHA256)
		if urlErr != nil || !strings.HasPrefix(artifact.URL, prefix) || !strings.HasSuffix(u.Path, ".zbplugin") ||
			strings.Contains(strings.TrimPrefix(artifact.URL, prefix), "/") || strings.Contains(u.Path, "..") ||
			hashErr != nil || len(sum) != 32 || artifact.Size <= 0 || artifact.Size > MaxPackageBytes ||
			!validPackagePlatform(artifact.Platform) || seen[artifact.Platform] {
			return nil, errors.New("invalid release artifact")
		}
		decoded, decodeErr := url.PathUnescape(strings.TrimPrefix(artifact.URL, prefix))
		if decodeErr != nil || strings.ContainsAny(decoded, "/\\") || strings.Contains(decoded, "..") {
			return nil, errors.New("invalid release artifact path")
		}
		seen[artifact.Platform] = true
	}
	return &MarketRelease{Version: version, Channel: channel, Artifacts: published.Artifacts}, nil
}

func withinListingBoundary(requested, admitted []string) bool {
	allowed := make(map[string]bool, len(admitted))
	for _, value := range admitted {
		allowed[value] = true
	}
	seen := map[string]bool{}
	for _, value := range requested {
		if seen[value] || !allowed[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func validPackagePlatform(platform string) bool {
	switch platform {
	case "any", "linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64", "windows-amd64", "windows-arm64":
		return true
	}
	return false
}

func (detail MarketDetail) hostArtifact() (MarketArtifact, error) {
	if detail.Release != nil {
		for _, artifact := range detail.Release.Artifacts {
			if artifact.Platform == detail.Platform {
				return artifact, nil
			}
		}
		for _, artifact := range detail.Release.Artifacts {
			if artifact.Platform == "any" {
				return artifact, nil
			}
		}
	}
	return MarketArtifact{}, errors.New("当前版本没有可用于此服务器的安装包")
}
