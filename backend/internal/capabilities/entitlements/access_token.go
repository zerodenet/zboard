package entitlements

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

type AccessCipher interface {
	Encrypt(string) (string, error)
	Decrypt(string) (string, error)
}

func HashAccessToken(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}
func NewAccessToken() (string, string, string, error) {
	entropy := make([]byte, 32)
	if _, err := rand.Read(entropy); err != nil {
		return "", "", "", err
	}
	raw := base64.RawURLEncoding.EncodeToString(entropy)
	return raw, HashAccessToken(raw), raw[:12], nil
}
func RecoverAccessToken(cipher AccessCipher, protected, hash string) (string, error) {
	if protected == "" {
		return "", errors.New("subscription token is not recoverable; rotate it to generate a new link")
	}
	raw, err := cipher.Decrypt(protected)
	if err != nil {
		return "", fmt.Errorf("decrypt subscription token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(HashAccessToken(raw)), []byte(hash)) != 1 {
		return "", errors.New("subscription token integrity check failed")
	}
	return raw, nil
}
