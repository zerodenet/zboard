package plugins

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

const DefaultRegistryURL = "https://raw.githubusercontent.com/zerodenet/plugins/main/catalogs/zboard.json"

type MarketReleaseSource struct {
	Type          string `json:"type"`
	MetadataAsset string `json:"metadata_asset"`
}

// The official directory admits publisher identities and policy ceilings.
// Version and artifact records remain in each publisher repository.
func (m *Manager) publicRegistry(ctx context.Context) (Market, error) {
	raw, err := m.fetch(ctx, DefaultRegistryURL, 2<<20)
	if err != nil {
		return Market{}, err
	}
	return parseRegistry(raw)
}

func parseRegistry(raw []byte) (Market, error) {
	var registry struct {
		SchemaVersion int    `json:"schema_version"`
		Host          string `json:"host"`
		Plugins       []struct {
			ID            string              `json:"id"`
			Repository    string              `json:"repository"`
			Publisher     registryPublisher   `json:"publisher"`
			Name          string              `json:"name"`
			Description   string              `json:"description"`
			License       string              `json:"license"`
			Maintainers   []string            `json:"maintainers"`
			Homepage      string              `json:"homepage"`
			Documentation string              `json:"documentation"`
			Security      string              `json:"security"`
			ReleaseSource MarketReleaseSource `json:"release_source"`
			Surfaces      []string            `json:"surfaces"`
			Capabilities  []string            `json:"capabilities"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &registry); err != nil {
		return Market{}, err
	}
	if registry.SchemaVersion != 2 || registry.Host != "zboard" || len(registry.Plugins) > 2000 {
		return Market{}, errors.New("invalid public plugin directory")
	}
	market := Market{Kind: "registry", Configured: true, SourceURL: DefaultRegistryURL, Entries: []MarketEntry{}}
	seen := map[string]bool{}
	for _, item := range registry.Plugins {
		u, err := safeRemoteURL(item.Repository)
		if err != nil || u.Host != "github.com" || len(strings.Split(strings.Trim(u.Path, "/"), "/")) != 2 {
			return Market{}, errors.New("invalid registry repository")
		}
		key, err := base64.StdEncoding.DecodeString(item.Publisher.PublicKey)
		if err != nil || len(key) != 32 || item.Publisher.ID == "" || len(item.Publisher.ID) > 160 {
			return Market{}, errors.New("invalid registry publisher")
		}
		if !idPattern.MatchString(item.ID) || seen[item.ID] ||
			item.ReleaseSource.Type != "github-releases" ||
			!safeMetadataAsset(item.ReleaseSource.MetadataAsset) {
			return Market{}, errors.New("invalid registry plugin")
		}
		if !validListingBoundary(item.Surfaces, item.Capabilities) {
			return Market{}, errors.New("invalid registry capability boundary")
		}
		if err := validateDirectoryInformation(item.Name, item.Description, item.License, item.Maintainers, item.Homepage, item.Documentation, item.Security); err != nil {
			return Market{}, err
		}
		seen[item.ID] = true
		market.Entries = append(market.Entries, MarketEntry{
			ID:        item.ID,
			Publisher: item.Publisher.ID, PublicKey: item.Publisher.PublicKey,
			Repository: item.Repository, ReleaseSource: item.ReleaseSource,
			Name: item.Name, Description: item.Description, License: item.License,
			Maintainers: append([]string(nil), item.Maintainers...), Homepage: item.Homepage,
			Documentation: item.Documentation, Security: item.Security,
			Surfaces:     append([]string(nil), item.Surfaces...),
			Capabilities: append([]string(nil), item.Capabilities...),
		})
	}
	return market, nil
}

type registryPublisher struct {
	ID        string `json:"id"`
	PublicKey string `json:"public_key"`
}

func safeMetadataAsset(value string) bool {
	if value == "" || len(value) > 128 || !strings.HasSuffix(value, ".json") ||
		strings.ContainsAny(value, "/\\\r\n") || strings.Contains(value, "..") {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || strings.ContainsRune("._-", r)) {
			return false
		}
	}
	return true
}

func validListingBoundary(surfaces, capabilities []string) bool {
	seen := map[string]bool{}
	for _, surface := range surfaces {
		if seen["surface:"+surface] || !slicesContains([]string{"public", "account", "admin"}, surface) {
			return false
		}
		seen["surface:"+surface] = true
	}
	for _, capability := range capabilities {
		if seen["capability:"+capability] || !strings.HasPrefix(capability, "zboard.") || !idPattern.MatchString(capability) {
			return false
		}
		seen["capability:"+capability] = true
	}
	return true
}

func slicesContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validateDirectoryInformation(name, description, license string, maintainers []string, links ...string) error {
	if strings.TrimSpace(name) == "" || len(name) > 160 || strings.TrimSpace(description) == "" || len(description) > 2000 || strings.TrimSpace(license) == "" || len(license) > 160 || len(maintainers) == 0 || len(maintainers) > 20 {
		return errors.New("invalid directory basic information")
	}
	seen := map[string]bool{}
	for _, value := range maintainers {
		if strings.TrimSpace(value) == "" || len(value) > 160 || seen[value] {
			return errors.New("invalid directory maintainer")
		}
		seen[value] = true
	}
	for _, link := range links {
		if link != "" {
			if _, err := safeRemoteURL(link); err != nil {
				return errors.New("invalid directory information URL")
			}
		}
	}
	return nil
}
