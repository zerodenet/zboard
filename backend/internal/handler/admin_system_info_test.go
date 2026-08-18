package handler

import (
	"testing"
	"time"
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

	info := buildAdminSystemInfo("0.1.0-rc.2", now.Add(-24*time.Hour), now)
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
