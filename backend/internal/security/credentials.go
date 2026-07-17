package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

const credentialPrefix = "zboard:v1:"

type CredentialCipher struct {
	aead cipher.AEAD
}

func NewCredentialCipher(encodedKey string) (*CredentialCipher, error) {
	key, err := decodeCredentialKey(encodedKey)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create credential cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create credential AEAD: %w", err)
	}
	return &CredentialCipher{aead: aead}, nil
}

func ValidateCredentialKey(encodedKey string) error {
	_, err := decodeCredentialKey(encodedKey)
	return err
}

func (c *CredentialCipher) Encrypt(plaintext string) (string, error) {
	if c == nil || c.aead == nil {
		return "", errors.New("credential cipher is not configured")
	}
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate credential nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return credentialPrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (c *CredentialCipher) Decrypt(encoded string) (string, error) {
	if c == nil || c.aead == nil {
		return "", errors.New("credential cipher is not configured")
	}
	if encoded == "" {
		return "", nil
	}
	if !IsEncryptedCredential(encoded) {
		return "", errors.New("credential is not encrypted")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(encoded, credentialPrefix))
	if err != nil {
		return "", errors.New("credential ciphertext is malformed")
	}
	if len(payload) < c.aead.NonceSize() {
		return "", errors.New("credential ciphertext is truncated")
	}
	nonce, ciphertext := payload[:c.aead.NonceSize()], payload[c.aead.NonceSize():]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", errors.New("credential decryption failed")
	}
	return string(plaintext), nil
}

func IsEncryptedCredential(value string) bool {
	return strings.HasPrefix(value, credentialPrefix)
}

func decodeCredentialKey(encodedKey string) ([]byte, error) {
	value := strings.TrimSpace(encodedKey)
	if value == "" {
		return nil, errors.New("credential_encryption_key is required")
	}

	decoders := []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
		hex.DecodeString,
	}
	for _, decode := range decoders {
		decoded, err := decode(value)
		if err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}
	return nil, errors.New("credential_encryption_key must encode exactly 32 random bytes")
}
