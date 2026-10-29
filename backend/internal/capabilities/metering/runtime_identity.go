package metering

import (
	"crypto/sha256"
	"encoding/hex"
)

// Auxiliary storage/read budget, conservatively above the existing1MiB Zero
// HTTP body limit. This does not change authenticated request admission.
// Internal auxiliary entrypoints must not materialize larger stored identities.
// This never replaces the original identity in financial proof or control calls.
const MaxRuntimeIdentityBytes = 2 * 1024 * 1024

func RuntimeIdentityDigest(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
