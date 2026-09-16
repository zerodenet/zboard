package plugins

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

var marketProductIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,159}$`)

func parseMarketplacePage(raw []byte, expectedChannel, hostVersion, _, _ string) (Market, string, time.Time, error) {
	if expectedChannel != "stable" && expectedChannel != "rc" && expectedChannel != "dev" {
		return Market{}, "", time.Time{}, errors.New("unsupported marketplace channel")
	}
	var page struct {
		SnapshotVersion string `json:"snapshot_version"`
		GeneratedAt     string `json:"generated_at"`
		Page            int    `json:"page"`
		PageSize        int    `json:"page_size"`
		Total           int    `json:"total"`
		Items           []struct {
			ID          string `json:"id"`
			ReleaseFeed []struct {
				Tag  string `json:"tag"`
				Name string `json:"name"`
				Body string `json:"body"`
				URL  string `json:"url"`
			} `json:"release_feed"`
			Name          string            `json:"name"`
			Description   string            `json:"description"`
			License       string            `json:"license"`
			Repository    string            `json:"repository"`
			Maintainers   []string          `json:"maintainers"`
			Homepage      string            `json:"homepage"`
			Documentation string            `json:"documentation"`
			Security      string            `json:"security"`
			Publisher     registryPublisher `json:"publisher"`
			Targets       []snapshotTarget  `json:"targets"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &page); err != nil || !strings.HasPrefix(page.SnapshotVersion, "sha256:") || !digestPattern.MatchString(strings.TrimPrefix(page.SnapshotVersion, "sha256:")) || page.Page != 1 || page.PageSize != 1000 || page.Total != len(page.Items) || page.Total < 0 || page.Total > 1000 {
		return Market{}, "", time.Time{}, errors.New("invalid marketplace snapshot page")
	}
	generatedAt, err := time.Parse(time.RFC3339, page.GeneratedAt)
	if err != nil || generatedAt.After(time.Now().UTC().Add(10*time.Minute)) {
		return Market{}, "", time.Time{}, errors.New("invalid marketplace snapshot timestamp")
	}
	market := Market{Kind: "registry", Configured: true, Entries: []MarketEntry{}}
	seenProducts := map[string]bool{}
	seenPackages := map[string]bool{}
	for _, item := range page.Items {
		if !marketProductIDPattern.MatchString(item.ID) || len(item.ID) > 160 || seenProducts[item.ID] {
			return Market{}, "", time.Time{}, errors.New("invalid marketplace product")
		}
		key, decodeErr := base64.StdEncoding.DecodeString(item.Publisher.PublicKey)
		repository, urlErr := safeRemoteURL(item.Repository)
		if decodeErr != nil || len(key) != 32 || item.Publisher.ID == "" || len(item.Publisher.ID) > 160 || urlErr != nil || repository.Host != "github.com" || len(strings.Split(strings.Trim(repository.Path, "/"), "/")) != 2 {
			return Market{}, "", time.Time{}, errors.New("invalid marketplace identity")
		}
		if err := validateDirectoryInformation(item.Name, item.Description, item.License, item.Maintainers, item.Homepage, item.Documentation, item.Security); err != nil {
			return Market{}, "", time.Time{}, err
		}
		if len(item.Targets) != 1 || item.Targets[0].Host != "zboard" || !idPattern.MatchString(item.Targets[0].PackageID) || len(item.Targets[0].PackageID) > 160 || seenPackages[item.Targets[0].PackageID] || len(item.Targets[0].Releases) == 0 || !validListingBoundary(item.Targets[0].Surfaces, item.Targets[0].Capabilities) {
			return Market{}, "", time.Time{}, errors.New("invalid marketplace host target")
		}
		target := item.Targets[0]
		entry := MarketEntry{ID: target.PackageID, ProductID: item.ID, Name: item.Name, Description: item.Description, License: item.License, Maintainers: append([]string(nil), item.Maintainers...), Homepage: item.Homepage, Documentation: item.Documentation, Security: item.Security, Repository: item.Repository, Publisher: item.Publisher.ID, PublicKey: item.Publisher.PublicKey, ReleaseSource: MarketReleaseSource{Type: "marketplace-snapshot", MetadataAsset: "marketplace-entry.json"}, Surfaces: append([]string{}, target.Surfaces...), Capabilities: append([]string(nil), target.Capabilities...)}
		if len(target.Releases) > 100 {
			return Market{}, "", time.Time{}, errors.New("marketplace release limit exceeded")
		}
		versions := map[string]bool{}
		for _, release := range target.Releases {

			if versions[release.Version] {
				return Market{}, "", time.Time{}, errors.New("duplicate marketplace release")
			}
			versions[release.Version] = true
			converted, err := convertMarketplaceRelease(entry, release, expectedChannel, hostVersion)
			if err != nil {
				return Market{}, "", time.Time{}, err
			}
			if converted != nil {
				for _, feed := range item.ReleaseFeed {
					if feed.Tag == "v"+release.Version && feed.URL == release.NotesURL {
						converted.Title = boundedMarketText(feed.Name, "", 200)
						converted.Notes = boundedMarketText(feed.Body, "", 20_000)
						break
					}
				}
				entry.releases = append(entry.releases, *converted)
			}
		}
		seenProducts[item.ID] = true
		seenPackages[target.PackageID] = true
		market.Entries = append(market.Entries, entry)
	}
	return market, page.SnapshotVersion, generatedAt, nil
}
