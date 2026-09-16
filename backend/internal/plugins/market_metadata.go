package plugins

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

func parsePublisherRelease(raw []byte, entry MarketEntry, selectedVersion string, hostVersions ...string) (*MarketRelease, error) {
	var envelope struct {
		SchemaVersion int             `json:"schema_version"`
		Release       json.RawMessage `json:"release"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.SchemaVersion != 0 || len(envelope.Release) > 0 {
		host := ""
		if len(hostVersions) > 0 {
			host = hostVersions[0]
		}
		return parseUnifiedPublisherRelease(raw, entry, selectedVersion, host)
	}
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
