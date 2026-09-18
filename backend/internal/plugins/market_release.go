package plugins

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"runtime"
	"strings"
	"time"
)

type MarketArtifact struct {
	Platform string `json:"platform"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}
type MarketRelease struct {
	Version      string           `json:"version"`
	Channel      string           `json:"channel"`
	Title        string           `json:"title,omitempty"`
	Notes        string           `json:"notes,omitempty"`
	URL          string           `json:"url,omitempty"`
	PublishedAt  time.Time        `json:"published_at,omitzero"`
	Artifacts    []MarketArtifact `json:"artifacts"`
	surfaces     []string
	capabilities []string
	bounded      bool
	metadata     string
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
	if entry.ReleaseSource.Type == "marketplace-snapshot" {
		if len(entry.releases) == 0 {
			if strings.TrimSpace(requestedVersion) != "" {
				return MarketDetail{}, errors.New("所选版本不适用于当前 ZBoard 版本")
			}
			detail.Notice = "暂无适用于当前 ZBoard 版本的发行包，请检查插件的版本要求。"
			return detail, nil
		}
		detail.Releases = append([]MarketRelease(nil), entry.releases...)
		detail.Release, err = selectMarketRelease(detail.Releases, requestedVersion)
		if err != nil {
			return MarketDetail{}, err
		}
		if _, artifactErr := detail.hostArtifact(); artifactErr != nil {
			detail.Notice = "此版本没有适用于当前服务器的安装包，或超过 32 MiB 安装上限。你仍可下载发行包。"
		}
		return detail, nil
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
		parsed, fetchErr = parsePublisherRelease(raw, *entry, detail.Release.Version, m.host)
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
		detail.Notice = "此版本没有适用于当前服务器的安装包，或超过 32 MiB 安装上限。你仍可下载发行包。"
	}
	return detail, nil
}
