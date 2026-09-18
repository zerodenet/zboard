package plugins

import (
	"errors"
	"net/url"
	"strings"

	"github.com/Masterminds/semver/v3"
)

func requestedMarketVersion(fallback, requested string) string {
	if strings.TrimSpace(requested) == "" {
		return strings.TrimPrefix(fallback, "v")
	}
	return strings.TrimPrefix(strings.TrimSpace(requested), "v")
}

func marketReleaseChannel(version string) (string, error) {
	value, err := semver.StrictNewVersion(strings.TrimPrefix(version, "v"))
	if err != nil {
		return "", errors.New("invalid market release version")
	}
	pre := strings.ToLower(value.Prerelease())
	switch {
	case pre == "":
		return "stable", nil
	case marketPrereleaseMatches(pre, "rc"):
		return "rc", nil
	case marketPrereleaseMatches(pre, "dev"):
		return "dev", nil
	default:
		return "", errors.New("unsupported market release channel")
	}
}

func marketPrereleaseMatches(prerelease, channel string) bool {
	if prerelease == channel || strings.HasPrefix(prerelease, channel+".") {
		return true
	}
	suffix := strings.TrimPrefix(prerelease, channel)
	return suffix != prerelease && suffix != "" && suffix[0] >= '0' && suffix[0] <= '9'
}

func selectMarketRelease(releases []MarketRelease, requested string) (*MarketRelease, error) {
	version := strings.TrimPrefix(strings.TrimSpace(requested), "v")
	if version == "" {
		for _, channel := range []string{"stable", "rc", "dev"} {
			for i := range releases {
				if releases[i].Channel == channel {
					selected := releases[i]
					return &selected, nil
				}
			}
		}
		return nil, errors.New("plugin has no selectable release")
	}
	if _, err := marketReleaseChannel(version); err != nil {
		return nil, err
	}
	for i := range releases {
		if releases[i].Version == version {
			selected := releases[i]
			return &selected, nil
		}
	}
	return nil, errors.New("selected plugin version is not published")
}

func validPackagePlatform(platform string) bool {
	switch platform {
	case "any", "linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64", "windows-amd64", "windows-arm64":
		return true
	}
	return false
}

func (detail MarketDetail) hostArtifact() (MarketArtifact, error) {
	if detail.Release != nil {
		for _, artifact := range detail.Release.Artifacts {
			if artifact.Platform == detail.Platform && artifact.Size <= MaxPackageBytes {
				return artifact, nil
			}
		}
		for _, artifact := range detail.Release.Artifacts {
			_, arch, _ := strings.Cut(detail.Platform, "-")
			if artifact.Platform == "any-"+arch && artifact.Size <= MaxPackageBytes {
				return artifact, nil
			}
		}
		for _, artifact := range detail.Release.Artifacts {
			if artifact.Platform == "any" && artifact.Size <= MaxPackageBytes {
				return artifact, nil
			}
		}
	}
	return MarketArtifact{}, errors.New("当前版本没有可用于此服务器的安装包（单包上限 32 MiB）")
}

func safeMarketArtifactPath(raw, prefix string) bool {
	if !strings.HasPrefix(raw, prefix) {
		return false
	}
	name, err := url.PathUnescape(strings.TrimPrefix(raw, prefix))
	return err == nil && name != "" && !strings.ContainsAny(name, "/\\\x00") && !strings.Contains(name, "..") && strings.HasSuffix(name, ".zbplugin")
}
