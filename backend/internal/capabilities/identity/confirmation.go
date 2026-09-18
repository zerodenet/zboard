package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type PasswordConfirmation struct {
	Key []byte
	Now func() time.Time
}

func (c PasswordConfirmation) Digest(id uint, hash, authorization, expires string) string {
	digest := sha256.Sum256([]byte(hash))
	mac := hmac.New(sha256.New, c.Key)
	fmt.Fprintf(mac, "identity-confirmation\n%d\n%s\n%x\n%s", id, authorization, digest, expires)
	return hex.EncodeToString(mac.Sum(nil))
}
func (c PasswordConfirmation) Valid(account Account, authorization, proof string) bool {
	if account.ID == 0 || account.PasswordHash == "!external" || len(c.Key) == 0 || authorization == "" {
		return false
	}
	expiry, signature, ok := strings.Cut(proof, ".")
	expires, err := strconv.ParseInt(expiry, 10, 64)
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	return ok && err == nil && expires > now.Unix() && expires <= now.Unix()+300 && hmac.Equal([]byte(signature), []byte(c.Digest(account.ID, account.PasswordHash, authorization, expiry)))
}
