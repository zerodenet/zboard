package version

import "testing"

func TestFullVersion(t *testing.T) {
	originalVersion, originalCommit, originalBuildTime := Version, Commit, BuildTime
	t.Cleanup(func() {
		Version, Commit, BuildTime = originalVersion, originalCommit, originalBuildTime
	})

	Version, Commit, BuildTime = "v0.0.1", "dev", "ignored"
	if got := FullVersion(); got != "v0.0.1" {
		t.Fatalf("FullVersion() = %q, want %q", got, "v0.0.1")
	}

	Commit, BuildTime = "abc123", "2026-07-17T00:00:00Z"
	if got := FullVersion(); got != "v0.0.1-abc123@2026-07-17T00:00:00Z" {
		t.Fatalf("FullVersion() = %q", got)
	}
}
