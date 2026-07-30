package version

// Build-time variables can be replaced via -ldflags.
var (
	Version   = "v0.0.1-dev.5"
	Commit    = "dev"
	BuildTime = "1970-01-01T00:00:00Z"
)

func FullVersion() string {
	if Commit == "" || Commit == "dev" {
		return Version
	}
	return Version + "-" + Commit + "@" + BuildTime
}
