package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Masterminds/semver/v3"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

type MarketEntry struct {
	DiscoveryOnly bool                `json:"discovery_only,omitempty"`
	Repository    string              `json:"repository,omitempty"`
	PublicKey     string              `json:"public_key,omitempty"`
	ReleaseSource MarketReleaseSource `json:"release_source,omitzero"`
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Description   string              `json:"description"`
	License       string              `json:"license,omitempty"`
	Maintainers   []string            `json:"maintainers,omitempty"`
	Homepage      string              `json:"homepage,omitempty"`
	Documentation string              `json:"documentation,omitempty"`
	Security      string              `json:"security,omitempty"`
	Version       string              `json:"version,omitempty"`
	Publisher     string              `json:"publisher"`
	PackageURL    string              `json:"package_url,omitempty"`
	SHA256        string              `json:"sha256,omitempty"`
	Surfaces      []string            `json:"surfaces"`
	Capabilities  []string            `json:"capabilities,omitempty"`
}
type Market struct {
	Kind       string        `json:"kind"`
	SourceURL  string        `json:"source_url,omitempty"`
	Configured bool          `json:"configured"`
	Entries    []MarketEntry `json:"entries"`
	ExpiresAt  time.Time     `json:"expires_at"`
}
type signedCatalog struct {
	Payload   json.RawMessage `json:"payload"`
	Signature Signature       `json:"signature"`
}
type marketPayload struct {
	SchemaVersion int           `json:"schema_version"`
	ExpiresAt     time.Time     `json:"expires_at"`
	Entries       []MarketEntry `json:"entries"`
}

func safeRemoteURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return nil, errors.New("public HTTPS URL required")
	}
	if u.Port() != "" && u.Port() != "443" {
		return nil, errors.New("HTTPS port 443 required")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !publicIP(ip) {
		return nil, errors.New("private destination denied")
	}
	return u, nil
}
func publicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified() && !net.IPv4(100, 64, 0, 0).Equal(ip) && !sharedIP(ip)
}
func sharedIP(ip net.IP) bool {
	_, network, _ := net.ParseCIDR("100.64.0.0/10")
	return network.Contains(ip)
}
func remoteClient() *http.Client {
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, errors.New("no destination address")
		}
		for _, ip := range ips {
			if !publicIP(ip.IP) {
				return nil, errors.New("private market destination denied")
			}
		}
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: validateMarketRedirect}
}
func fetchRemote(ctx context.Context, raw string, max int64) ([]byte, error) {
	if _, err := safeRemoteURL(raw); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	client := remoteClient()
	defer client.CloseIdleConnections()
	res, err := client.Do(req)
	if err != nil {
		return nil, errors.New("market request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, errors.New("market returned HTTP " + strconv.Itoa(res.StatusCode))
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New("market response too large")
	}
	return b, nil
}
func parseMarket(raw []byte, keys map[string]string, now time.Time) (Market, error) {
	var signed signedCatalog
	if err := DecodeStrict(raw, &signed); err != nil {
		return Market{}, err
	}
	if err := verifySignature(signed.Payload, signed.Signature, keys); err != nil {
		return Market{}, err
	}
	var payload marketPayload
	if err := DecodeStrict(signed.Payload, &payload); err != nil {
		return Market{}, err
	}
	if payload.SchemaVersion != 1 || !payload.ExpiresAt.After(now) || payload.ExpiresAt.After(now.Add(31*24*time.Hour)) || len(payload.Entries) > 200 {
		return Market{}, errors.New("invalid or expired market catalog")
	}
	seen := map[string]bool{}
	for _, e := range payload.Entries {
		if !idPattern.MatchString(e.ID) || !digestPattern.MatchString(e.SHA256) || seen[e.ID] || len(e.Description) > 2000 || len(e.Name) > 160 || e.Publisher == "" || len(e.Publisher) > 160 {
			return Market{}, errors.New("invalid catalog entry")
		}
		key := e.PublicKey
		if key == "" {
			key = keys[e.Publisher]
		}
		decoded, err := base64.StdEncoding.DecodeString(key)
		if err != nil || len(decoded) != 32 || (keys[e.Publisher] != "" && keys[e.Publisher] != key) {
			return Market{}, errors.New("invalid catalog publisher key")
		}
		if _, err := semver.StrictNewVersion(e.Version); err != nil || e.Name == "" || len(e.ID) > 160 || len(e.Surfaces) > 3 {
			return Market{}, errors.New("invalid catalog version or surfaces")
		}
		surfaces := map[string]bool{}
		for _, s := range e.Surfaces {
			if surfaces[s] || !slices.Contains([]string{"public", "account", "admin"}, s) {
				return Market{}, errors.New("invalid catalog surface")
			}
			surfaces[s] = true
		}
		if _, err := safeRemoteURL(e.PackageURL); err != nil {
			return Market{}, err
		}
		seen[e.ID] = true
	}
	if payload.Entries == nil {
		payload.Entries = []MarketEntry{}
	}
	return Market{Kind: "signed", Configured: true, Entries: payload.Entries, ExpiresAt: payload.ExpiresAt}, nil
}
func (m *Manager) Market(ctx context.Context) (Market, error) {
	if m.options.CatalogURL == "" {
		return m.publicRegistry(ctx)
	}
	raw, err := m.fetch(ctx, m.options.CatalogURL, 2<<20)
	if err != nil {
		return Market{}, err
	}
	return parseMarket(raw, m.options.TrustedPublishers, time.Now().UTC())
}
func (m *Manager) InstallMarket(ctx context.Context, id, digest, actor string) (Installation, error) {
	if m.options.CatalogURL == "" {
		return Installation{}, errors.New("公开目录仅用于发现插件，在线安装需要已签名的安装目录；也可下载插件包后离线导入")
	}
	market, err := m.Market(ctx)
	if err != nil {
		return Installation{}, err
	}
	for _, e := range market.Entries {
		if e.ID != id {
			continue
		}
		if e.SHA256 != digest {
			return Installation{}, ErrConflict
		}
		raw, err := m.fetch(ctx, e.PackageURL, MaxPackageBytes)
		if err != nil {
			return Installation{}, err
		}
		sum := sha256.Sum256(raw)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), e.SHA256) {
			return Installation{}, errors.New("market package checksum mismatch")
		}
		key := e.PublicKey
		if key == "" {
			key = m.options.TrustedPublishers[e.Publisher]
		}
		p, err := ReadPackage(raw, map[string]string{e.Publisher: key})
		if err != nil {
			return Installation{}, err
		}
		if p.Manifest.ID != e.ID || p.Manifest.Version != e.Version || p.Publisher != e.Publisher {
			return Installation{}, errors.New("market package identity mismatch")
		}
		return m.importVerified(raw, actor, p, true)
	}
	return Installation{}, errors.New("plugin not found in configured market")
}
