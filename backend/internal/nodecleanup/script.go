// Package nodecleanup embeds the standalone, offline node maintenance utility.
package nodecleanup

import _ "embed"

//go:embed cleanup-zero-node.sh
var Script []byte
