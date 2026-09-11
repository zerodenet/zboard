package plugins

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type publisherMetadata struct {
	SchemaVersion int      `json:"schema_version"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Repository    string   `json:"repository"`
	License       string   `json:"license"`
	Maintainers   []string `json:"maintainers"`
	Homepage      string   `json:"homepage,omitempty"`
	Documentation string   `json:"documentation,omitempty"`
	Security      string   `json:"security,omitempty"`
}

type marketMetadataCache struct {
	expiresAt time.Time
	metadata  publisherMetadata
}

func (m *Manager) publisherMetadata(ctx context.Context, entry MarketEntry) (publisherMetadata, error) {
	repository := strings.TrimPrefix(strings.TrimRight(entry.Repository, "/"), "https://github.com/")
	if repository == entry.Repository || strings.Count(repository, "/") != 1 ||
		entry.MetadataSource.Type != "repository-file" || entry.MetadataSource.Path != "marketplace.json" {
		return publisherMetadata{}, errors.New("unsupported plugin metadata source")
	}
	cacheKey := entry.Repository + "\n" + entry.MetadataSource.Path
	m.marketMu.Lock()
	cached, ok := m.marketMetadata[cacheKey]
	m.marketMu.Unlock()
	if ok && cached.expiresAt.After(time.Now()) {
		return cached.metadata, nil
	}
	raw, err := m.fetch(ctx, "https://api.github.com/repos/"+repository+"/contents/"+entry.MetadataSource.Path, 512<<10)
	if err != nil {
		return publisherMetadata{}, err
	}
	var content struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
		Size     int64  `json:"size"`
	}
	if err := json.Unmarshal(raw, &content); err != nil || content.Type != "file" || content.Encoding != "base64" ||
		content.Size <= 0 || content.Size > 64<<10 {
		return publisherMetadata{}, errors.New("invalid plugin repository metadata response")
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(content.Content, "\n", ""))
	if err != nil || int64(len(decoded)) != content.Size {
		return publisherMetadata{}, errors.New("invalid plugin repository metadata content")
	}
	var metadata publisherMetadata
	if err := DecodeStrict(decoded, &metadata); err != nil {
		return publisherMetadata{}, err
	}
	if err := validatePublisherMetadata(metadata, entry); err != nil {
		return publisherMetadata{}, err
	}
	m.marketMu.Lock()
	m.marketMetadata[cacheKey] = marketMetadataCache{expiresAt: time.Now().Add(5 * time.Minute), metadata: metadata}
	m.marketMu.Unlock()
	return metadata, nil
}

func validatePublisherMetadata(metadata publisherMetadata, entry MarketEntry) error {
	if metadata.SchemaVersion != 1 || metadata.ID != entry.ID ||
		strings.TrimRight(metadata.Repository, "/") != strings.TrimRight(entry.Repository, "/") ||
		strings.TrimSpace(metadata.Name) == "" || len(metadata.Name) > 160 ||
		len(metadata.Description) > 2000 || strings.TrimSpace(metadata.License) == "" || len(metadata.License) > 160 ||
		len(metadata.Maintainers) == 0 || len(metadata.Maintainers) > 20 {
		return errors.New("invalid plugin repository metadata")
	}
	seen := map[string]bool{}
	for _, maintainer := range metadata.Maintainers {
		if strings.TrimSpace(maintainer) == "" || len(maintainer) > 160 || seen[maintainer] {
			return errors.New("invalid plugin repository maintainer")
		}
		seen[maintainer] = true
	}
	for _, target := range []string{metadata.Homepage, metadata.Documentation, metadata.Security} {
		if target == "" {
			continue
		}
		if _, err := safeRemoteURL(target); err != nil {
			return errors.New("invalid plugin repository metadata URL")
		}
	}
	return nil
}
