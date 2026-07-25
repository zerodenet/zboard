package migrations

import "embed"

// Files contains the v0.0.1 schema baseline and later append-only release
// migrations shipped with every zboard binary, so a deployment never depends
// on a separately copied schema folder.
//
//go:embed *.up.sql *.down.sql
var Files embed.FS
