package entitlements

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// RetiredPrincipalHash retains attribution for kernels whose authenticated
// principal is a credential secret (notably current Shadowsocks). It is not an
// authentication credential and must never be emitted in subscription output.
func RetiredPrincipalHash(node uint, secret string) string {
	if node == 0 || secret == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("zboard-retired-principal-v1\x00" + strconv.FormatUint(uint64(node), 10) + "\x00" + secret))
	return hex.EncodeToString(sum[:])
}
