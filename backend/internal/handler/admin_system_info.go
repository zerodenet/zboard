package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/version"
)

var zboardProcessStartedAt = time.Now().UTC()

type adminProjectLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type adminSystemInfo struct {
	Service        string             `json:"service"`
	Version        string             `json:"version"`
	ReleaseChannel string             `json:"release_channel"`
	StartedAt      time.Time          `json:"started_at"`
	UptimeSeconds  int64              `json:"uptime_seconds"`
	InstalledAt    time.Time          `json:"installed_at"`
	License        map[string]string  `json:"license"`
	Links          []adminProjectLink `json:"links"`
	UpdateURL      string             `json:"update_url"`
}

func (h *handlers) AdminSystemInfoHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	var installation model.Installation
	if err := h.db.First(&installation, 1).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, buildAdminSystemInfo(version.FullVersion(), installation.InstalledAt, time.Now().UTC()))
}

func buildAdminSystemInfo(currentVersion string, installedAt, now time.Time) adminSystemInfo {
	startedAt := zboardProcessStartedAt
	if now.Before(startedAt) {
		startedAt = now
	}
	return adminSystemInfo{
		Service:        "zboard",
		Version:        currentVersion,
		ReleaseChannel: zboardReleaseChannel(currentVersion),
		StartedAt:      startedAt,
		UptimeSeconds:  int64(now.Sub(startedAt).Seconds()),
		InstalledAt:    installedAt.UTC(),
		License: map[string]string{
			"spdx":    "MPL-2.0",
			"name":    "Mozilla Public License 2.0",
			"edition": "open-source",
		},
		Links: []adminProjectLink{
			{Label: "源码仓库", URL: "https://github.com/zerodenet/zboard"},
			{Label: "项目文档", URL: "https://docs.zerodenet.org"},
			{Label: "文档仓库", URL: "https://github.com/zerodenet/docs"},
			{Label: "版本发布", URL: "https://github.com/zerodenet/zboard/releases"},
			{Label: "问题反馈", URL: "https://github.com/zerodenet/zboard/issues"},
			{Label: "Telegram 社区", URL: "https://t.me/zerodenet"},
			{Label: "ZeroDeNet", URL: "https://github.com/zerodenet"},
		},
		UpdateURL: "https://github.com/zerodenet/zboard/releases",
	}
}

func zboardReleaseChannel(currentVersion string) string {
	value := strings.ToLower(strings.TrimSpace(currentVersion))
	switch {
	case strings.Contains(value, "dev"):
		return "development"
	case strings.Contains(value, "rc"):
		return "release-candidate"
	case strings.Contains(value, "alpha") || strings.Contains(value, "beta"):
		return "preview"
	default:
		return "stable"
	}
}
