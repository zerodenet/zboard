package handler

import (
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/version"
)

func TestZboardReleaseChannel(t *testing.T) {
	tests := map[string]string{
		"0.0.16-dev.20260817": "development",
		"0.1.0-rc.2":          "release-candidate",
		"0.1.0-beta.1":        "preview",
		"0.1.0":               "stable",
	}
	for version, want := range tests {
		if got := zboardReleaseChannel(version); got != want {
			t.Fatalf("zboardReleaseChannel(%q) = %q, want %q", version, got, want)
		}
	}
}

func TestBuildAdminSystemInfoPublishesOpenSourceResources(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	oldStartedAt := zboardProcessStartedAt
	zboardProcessStartedAt = now.Add(-2 * time.Hour)
	defer func() { zboardProcessStartedAt = oldStartedAt }()

	info := buildAdminSystemInfo("0.1.0-rc.2-deadbeef@2026-08-17T09:00:00Z", now.Add(-24*time.Hour), now)
	if info.Service != "ZBoard" {
		t.Fatalf("service = %q, want ZBoard", info.Service)
	}
	if info.ReleaseVersion != version.Version || info.Commit != version.Commit || info.BuildTime != version.BuildTime {
		t.Fatalf("structured build metadata = %#v", info)
	}
	if info.UptimeSeconds != 7200 {
		t.Fatalf("uptime_seconds = %d, want 7200", info.UptimeSeconds)
	}
	if info.License["spdx"] != "MPL-2.0" || info.License["edition"] != "open-source" {
		t.Fatalf("license = %#v", info.License)
	}
	if info.UpdateURL != "https://github.com/zerodenet/zboard/releases" {
		t.Fatalf("update_url = %q", info.UpdateURL)
	}
	if len(info.Links) < 5 {
		t.Fatalf("links = %#v, want project resources", info.Links)
	}
}
