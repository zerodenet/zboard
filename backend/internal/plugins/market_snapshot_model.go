package plugins

type snapshotArtifact struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type snapshotRelease struct {
	Version      string   `json:"version"`
	Channel      string   `json:"channel"`
	PublishedAt  string   `json:"published_at"`
	NotesURL     string   `json:"notes_url"`
	Surfaces     []string `json:"surfaces"`
	Capabilities []string `json:"capabilities"`
	HostVersion  struct {
		Min          string `json:"min"`
		MaxExclusive string `json:"max_exclusive"`
	} `json:"host_version"`
	Artifacts []snapshotArtifact `json:"artifacts"`
}
type snapshotTarget struct {
	Host         string            `json:"host"`
	PackageID    string            `json:"package_id"`
	Surfaces     []string          `json:"surfaces"`
	Capabilities []string          `json:"capabilities"`
	Releases     []snapshotRelease `json:"releases"`
}
