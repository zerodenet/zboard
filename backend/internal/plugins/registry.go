package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Masterminds/semver/v3"
)

const DefaultRegistryURL = "https://raw.githubusercontent.com/zerodenet/plugins/main/catalogs/zboard.json"

// The public registry is discovery metadata, never an installation trust root.
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
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Repository  string `json:"repository"`
			Publisher   struct {
				ID string `json:"id"`
			} `json:"publisher"`
			Source struct {
				Version string `json:"version"`
			} `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &registry); err != nil {
		return Market{}, err
	}
	if registry.SchemaVersion != 1 || registry.Host != "zboard" || len(registry.Plugins) > 2000 {
		return Market{}, errors.New("invalid public plugin registry")
	}
	market := Market{Kind: "registry", Configured: true, SourceURL: DefaultRegistryURL, Entries: []MarketEntry{}}
	seen := map[string]bool{}
	for _, item := range registry.Plugins {
		u, err := safeRemoteURL(item.Repository)
		if err != nil || u.Host != "github.com" || len(strings.Split(strings.Trim(u.Path, "/"), "/")) != 2 {
			return Market{}, errors.New("invalid registry repository")
		}
		version := strings.TrimPrefix(item.Source.Version, "v")
		if _, err := semver.StrictNewVersion(version); err != nil {
			return Market{}, errors.New("invalid registry version")
		}
		if !idPattern.MatchString(item.ID) || seen[item.ID] || item.Name == "" || len(item.Name) > 160 || len(item.Description) > 2000 || item.Publisher.ID == "" || len(item.Publisher.ID) > 160 {
			return Market{}, errors.New("invalid registry plugin")
		}
		seen[item.ID] = true
		market.Entries = append(market.Entries, MarketEntry{ID: item.ID, Name: item.Name, Description: item.Description, Version: version, Publisher: item.Publisher.ID, Repository: item.Repository, DiscoveryOnly: true, Surfaces: []string{}})
	}
	return market, nil
}
