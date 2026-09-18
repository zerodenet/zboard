package plugins

import (
	"errors"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

func convertMarketplaceRelease(entry MarketEntry, release snapshotRelease, expectedChannel, hostVersion string) (*MarketRelease, error) {
	if _, err := semver.StrictNewVersion(release.Version); err != nil {
		return nil, errors.New("invalid marketplace release version")
	}
	publishedAt, timeErr := time.Parse(time.RFC3339, release.PublishedAt)
	actualChannel, versionErr := marketReleaseChannel(release.Version)
	notes, notesErr := safeRemoteURL(release.NotesURL)
	compatible, compatibilityErr := marketplaceHostCompatible(release.HostVersion.Min, release.HostVersion.MaxExclusive, hostVersion)
	if versionErr != nil || timeErr != nil || release.Channel != expectedChannel || release.Channel != actualChannel || notesErr != nil || notes.Host != "github.com" || release.NotesURL != strings.TrimRight(entry.Repository, "/")+"/releases/tag/v"+release.Version || compatibilityErr != nil || !withinListingBoundary(release.Surfaces, entry.Surfaces) || !withinListingBoundary(release.Capabilities, entry.Capabilities) {
		return nil, errors.New("invalid marketplace release")
	}
	if len(release.Artifacts) == 0 || len(release.Artifacts) > 20 {
		return nil, errors.New("invalid marketplace artifact count")
	}
	converted := MarketRelease{Version: release.Version, Channel: release.Channel, URL: release.NotesURL, PublishedAt: publishedAt, Artifacts: []MarketArtifact{}, surfaces: release.Surfaces, capabilities: release.Capabilities, bounded: true}
	platforms := map[string]bool{}
	for _, artifact := range release.Artifacts {
		platform := artifact.OS + "-" + artifact.Arch
		if artifact.OS == "any" && artifact.Arch == "any" {
			platform = "any"
		}
		if !validSnapshotPlatform(artifact.OS, artifact.Arch) || platforms[platform] || artifact.Size <= 0 || artifact.Size > 128<<20 || !digestPattern.MatchString(artifact.SHA256) {
			return nil, errors.New("invalid marketplace artifact")
		}
		u, artifactErr := safeRemoteURL(artifact.URL)
		prefix := strings.TrimRight(entry.Repository, "/") + "/releases/download/v" + release.Version + "/"
		if artifactErr != nil || u.Host != "github.com" || !safeMarketArtifactPath(artifact.URL, prefix) {
			return nil, errors.New("invalid marketplace artifact URL")
		}
		platforms[platform] = true
		if compatible {
			converted.Artifacts = append(converted.Artifacts, MarketArtifact{Platform: platform, URL: artifact.URL, SHA256: artifact.SHA256, Size: artifact.Size})
		}
	}
	if compatible && len(converted.Artifacts) > 0 {
		return &converted, nil
	}
	return nil, nil
}

func marketplaceHostCompatible(minimum, maximumExclusive, hostVersion string) (bool, error) {
	minimumVersion, err := semver.StrictNewVersion(minimum)
	if err != nil {
		return false, err
	}
	var maximumVersion *semver.Version
	if maximumExclusive != "" {
		maximumVersion, err = semver.StrictNewVersion(maximumExclusive)
		if err != nil || !maximumVersion.GreaterThan(minimumVersion) {
			return false, errors.New("invalid marketplace host version range")
		}
	}

	current, err := semver.StrictNewVersion(strings.TrimPrefix(hostVersion, "v"))
	if err != nil {
		return false, nil
	}
	current = marketplaceHostReleaseLine(current)
	return !current.LessThan(minimumVersion) && (maximumVersion == nil || current.LessThan(maximumVersion)), nil
}

// Marketplace host ranges describe product release lines. An RC/dev build of
// 0.0.2 therefore consumes 0.0.2 listings, while 0.1.0-rc remains outside a
// max-exclusive 0.1.0 boundary. Package admission still uses its own protocol,
// capability, platform, lifecycle, and advisory publisher-coverage checks.
func marketplaceHostReleaseLine(version *semver.Version) *semver.Version {
	line, err := version.SetPrerelease("")
	if err != nil {
		return version
	}
	line, err = line.SetMetadata("")
	if err != nil {
		return version
	}
	return &line
}

func validSnapshotPlatform(os, arch string) bool {
	if arch != "amd64" && arch != "arm64" && !(os == "any" && arch == "any") {
		return false
	}
	switch os {
	case "any", "linux", "darwin", "windows", "android", "ios":
		return true
	}
	return false
}
