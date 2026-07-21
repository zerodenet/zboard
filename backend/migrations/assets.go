package migrations

import "embed"

// Files contains the ordered SQL migration chain shipped with every zboard
// binary, so a deployment never depends on a separately copied schema folder.
//
//go:embed *.up.sql *.down.sql
var Files embed.FS
