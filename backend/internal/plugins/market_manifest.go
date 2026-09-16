package plugins

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

var marketCommitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

func parseUnifiedPublisherRelease(raw []byte, entry MarketEntry, version, hostVersion string) (*MarketRelease, error) {
	var doc struct {
		SchemaVersion int               `json:"schema_version"`
		ProductID     string            `json:"product_id"`
		Repository    string            `json:"repository"`
		Publisher     registryPublisher `json:"publisher"`
		Source        struct {
			Tag    string `json:"tag"`
			Commit string `json:"commit"`
		} `json:"source"`
		Release struct {
			Version     string `json:"version"`
			Channel     string `json:"channel"`
			PublishedAt string `json:"published_at"`
			NotesURL    string `json:"notes_url"`
			Targets     []struct {
				Host      string `json:"host"`
				PackageID string `json:"package_id"`
				snapshotRelease
			} `json:"targets"`
		} `json:"release"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	version = strings.TrimPrefix(version, "v")
	if doc.SchemaVersion != 1 || !marketProductIDPattern.MatchString(doc.ProductID) ||
		(entry.ProductID != "" && doc.ProductID != entry.ProductID) || doc.Repository != entry.Repository ||
		doc.Publisher.ID != entry.Publisher || doc.Publisher.PublicKey != entry.PublicKey ||
		doc.Release.Version != version || doc.Source.Tag != "v"+version || !marketCommitPattern.MatchString(doc.Source.Commit) ||
		len(doc.Release.Targets) == 0 || len(doc.Release.Targets) > 2 {
		return nil, errors.New("release identity differs from marketplace admission")
	}
	var selected *snapshotRelease
	seen := map[string]bool{}
	for _, target := range doc.Release.Targets {
		if seen[target.Host] || (target.Host != "zboard" && target.Host != "znet-sink") {
			return nil, errors.New("invalid release host targets")
		}
		seen[target.Host] = true
		if target.Host != "zboard" {
			continue
		}
		if target.PackageID != entry.ID {
			return nil, errors.New("release package differs from admitted ZBoard package")
		}
		release := target.snapshotRelease
		release.Version, release.Channel = doc.Release.Version, doc.Release.Channel
		release.PublishedAt, release.NotesURL = doc.Release.PublishedAt, doc.Release.NotesURL
		selected = &release
	}
	if selected == nil {
		return nil, errors.New("release has no ZBoard package")
	}
	result, err := convertMarketplaceRelease(entry, *selected, doc.Release.Channel, hostVersion)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("release does not support this ZBoard version")
	}
	return result, nil
}
