package plugins

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"runtime"
	"strings"

	"gorm.io/gorm"
)

type MarketArtifact struct {
	Platform string `json:"platform"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}
type MarketRelease struct {
	Version   string           `json:"version"`
	Artifacts []MarketArtifact `json:"artifacts"`
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
	Installed *MarketInstalled `json:"installed,omitempty"`
	Notice    string           `json:"notice,omitempty"`
	// The publisher key is untrusted metadata until package inspection and confirmation.
	publicKey string
	signed    bool
}

func (m *Manager) MarketDetail(ctx context.Context, id string) (MarketDetail, error) {
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
	d := MarketDetail{Entry: *entry, Platform: runtime.GOOS + "-" + runtime.GOARCH, signed: market.Kind == "signed"}
	m.mu.Lock()
	installed, err := m.load(id)
	m.mu.Unlock()
	if err == nil {
		d.Installed = &MarketInstalled{Version: installed.Version, Digest: installed.Digest, State: installed.State}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return MarketDetail{}, err
	}
	if d.signed {
		d.publicKey = entry.PublicKey
		if d.publicKey == "" {
			d.publicKey = m.options.TrustedPublishers[entry.Publisher]
		}
		d.Release = &MarketRelease{Version: entry.Version, Artifacts: []MarketArtifact{{Platform: d.Platform, URL: entry.PackageURL, SHA256: entry.SHA256}}}
		return d, nil
	}
	metadataURL := strings.TrimRight(entry.Repository, "/") + "/releases/download/v" + entry.Version + "/marketplace-entry.json"
	raw, err := m.fetch(ctx, metadataURL, 256<<10)
	if err == nil {
		d.Release, d.publicKey, err = parsePublisherRelease(raw, *entry)
	}
	if err != nil {
		d.Notice = "无法读取此版本的发行信息，请刷新重试，或到插件仓库查看发布状态。"
		return d, nil
	}
	if _, err := d.hostArtifact(); err != nil {
		d.Notice = "此版本没有适用于当前服务器的安装包，可以下载其他平台的安装包。"
	}
	return d, nil
}

func parsePublisherRelease(raw []byte, entry MarketEntry) (*MarketRelease, string, error) {
	var doc struct {
		ID         string `json:"id"`
		Repository string `json:"repository"`
		Publisher  struct {
			ID        string `json:"id"`
			PublicKey string `json:"public_key"`
		} `json:"publisher"`
		Releases []MarketRelease `json:"releases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, "", err
	}
	if doc.ID != entry.ID || strings.TrimRight(doc.Repository, "/") != strings.TrimRight(entry.Repository, "/") || doc.Publisher.ID != entry.Publisher || len(doc.Releases) > 100 {
		return nil, "", errors.New("release identity mismatch")
	}
	key, err := base64.StdEncoding.DecodeString(doc.Publisher.PublicKey)
	if err != nil || len(key) != 32 {
		return nil, "", errors.New("invalid release signing key")
	}
	var selected *MarketRelease
	for _, release := range doc.Releases {
		if strings.TrimPrefix(release.Version, "v") != entry.Version {
			continue
		}
		if selected != nil {
			return nil, "", errors.New("duplicate release")
		}
		copy := release
		copy.Version = entry.Version
		selected = &copy
	}
	if selected == nil || len(selected.Artifacts) == 0 || len(selected.Artifacts) > 20 {
		return nil, "", errors.New("release has no packages")
	}
	prefix := strings.TrimRight(entry.Repository, "/") + "/releases/download/v" + entry.Version + "/"
	seen := map[string]bool{}
	for _, a := range selected.Artifacts {
		u, err := safeRemoteURL(a.URL)
		sum, hashErr := hex.DecodeString(a.SHA256)
		if err != nil || !strings.HasPrefix(a.URL, prefix) || !strings.HasSuffix(u.Path, ".zbplugin") || strings.Contains(strings.TrimPrefix(a.URL, prefix), "/") || strings.Contains(u.Path, "..") || hashErr != nil || len(sum) != 32 || a.Size <= 0 || a.Size > MaxPackageBytes || !validPackagePlatform(a.Platform) || seen[a.Platform] {
			return nil, "", errors.New("invalid release artifact")
		}
		// Reject escaped path separators and traversal before exposing download links.
		decoded, err := url.PathUnescape(strings.TrimPrefix(a.URL, prefix))
		if err != nil || strings.ContainsAny(decoded, "/\\") || strings.Contains(decoded, "..") {
			return nil, "", errors.New("invalid release artifact path")
		}
		seen[a.Platform] = true
	}
	return selected, doc.Publisher.PublicKey, nil
}
func validPackagePlatform(p string) bool {
	switch p {
	case "any", "linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64", "windows-amd64", "windows-arm64":
		return true
	}
	return false
}
func (d MarketDetail) hostArtifact() (MarketArtifact, error) {
	if d.Release != nil {
		for _, a := range d.Release.Artifacts {
			if a.Platform == d.Platform {
				return a, nil
			}
		}
		for _, a := range d.Release.Artifacts {
			if a.Platform == "any" {
				return a, nil
			}
		}
	}
	return MarketArtifact{}, errors.New("当前版本没有可用于此服务器的安装包")
}
