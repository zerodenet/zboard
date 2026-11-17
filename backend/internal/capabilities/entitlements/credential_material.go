package entitlements

import (
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const CredentialMaterialVersion = 1
const subscriptionPrincipalPrefix = "sp1:"

var ErrCredentialMigrationBlocked = errors.New("subscription credential migration blocked")

var ErrCredentialMaterial = errors.New("invalid subscription credential material")

// A seed belongs to one subscription entitlement generation. It is encrypted
// at rest and never sent to a client or a node. Node IDs are deliberately absent
// from connection-key derivation, so adding an endpoint creates no new secret.
func NewCredentialMaterial() (string, error) {
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(seed), nil
}

func credentialMaterial(seed, purpose string, size int) ([]byte, error) {
	raw, err := base64.RawStdEncoding.DecodeString(seed)
	if err != nil || len(raw) != 32 {
		return nil, ErrCredentialMaterial
	}
	return hkdf.Key(sha256.New, raw, nil, "zboard/subscription-credential/v1/"+purpose, size)
}

func RenderCredentialMaterial(seed, protocol, method string) (string, error) {
	profile, size := "password", 32
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "vmess", "vless":
		profile, size = "uuid", 16
	case "trojan", "hysteria2", "mieru":
	case "shadowsocks":
		method = strings.ToLower(strings.TrimSpace(method))
		if strings.HasPrefix(method, "2022-") {
			switch method {
			case "2022-blake3-aes-128-gcm":
				profile, size = "ss2022-128", 16
			case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
				profile = "ss2022-256"
			default:
				return "", ErrCredentialMaterial
			}
		}
	default:
		return "", ErrCredentialMaterial
	}
	raw, err := credentialMaterial(seed, profile, size)
	if err != nil {
		return "", err
	}
	if profile == "uuid" {
		raw[6] = (raw[6] & 0x0f) | 0x40
		raw[8] = (raw[8] & 0x3f) | 0x80
		value, err := uuid.FromBytes(raw)
		return value.String(), err
	}
	if strings.HasPrefix(profile, "ss2022-") {
		return base64.StdEncoding.EncodeToString(raw), nil
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func credentialSigningKey(seed string) (ed25519.PrivateKey, error) {
	raw, err := credentialMaterial(seed, "attribution-signing", ed25519.SeedSize)
	if err != nil {
		return nil, err
	}
	return ed25519.NewKeyFromSeed(raw), nil
}

// The public verifier is sufficient for arbitrarily late accounting after the
// usable connection seed has been erased. There is no per-endpoint secret or
// per-endpoint attribution row for new identities.
func CredentialMaterialVerifier(seed string) (string, error) {
	key, err := credentialSigningKey(seed)
	if err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)), nil
}

type CredentialBinding struct{ CredentialID, NodeID, EndpointID uint }

func CredentialBindingPrincipal(seed string, binding CredentialBinding) (string, error) {
	if binding.CredentialID == 0 || binding.NodeID == 0 || binding.EndpointID == 0 {
		return "", ErrCredentialMaterial
	}
	key, err := credentialSigningKey(seed)
	if err != nil {
		return "", err
	}
	claims := make([]byte, 24)
	binary.BigEndian.PutUint64(claims, uint64(binding.CredentialID))
	binary.BigEndian.PutUint64(claims[8:], uint64(binding.NodeID))
	binary.BigEndian.PutUint64(claims[16:], uint64(binding.EndpointID))
	signature := ed25519.Sign(key, append([]byte(subscriptionPrincipalPrefix), claims...))
	return subscriptionPrincipalPrefix + base64.RawURLEncoding.EncodeToString(append(claims, signature...)), nil
}

func ParseCredentialBinding(principal string) (CredentialBinding, error) {
	if !strings.HasPrefix(principal, subscriptionPrincipalPrefix) {
		return CredentialBinding{}, ErrCredentialMaterial
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(principal, subscriptionPrincipalPrefix))
	if err != nil || len(raw) != 24+ed25519.SignatureSize {
		return CredentialBinding{}, ErrCredentialMaterial
	}
	b := CredentialBinding{uint(binary.BigEndian.Uint64(raw)), uint(binary.BigEndian.Uint64(raw[8:])), uint(binary.BigEndian.Uint64(raw[16:]))}
	if b.CredentialID == 0 || b.NodeID == 0 || b.EndpointID == 0 || uint64(b.CredentialID) != binary.BigEndian.Uint64(raw) || uint64(b.NodeID) != binary.BigEndian.Uint64(raw[8:]) || uint64(b.EndpointID) != binary.BigEndian.Uint64(raw[16:]) {
		return CredentialBinding{}, ErrCredentialMaterial
	}
	return b, nil
}

func VerifyCredentialBinding(verifier, principal string) (CredentialBinding, error) {
	binding, err := ParseCredentialBinding(principal)
	if err != nil {
		return binding, err
	}
	key, err := base64.RawStdEncoding.DecodeString(verifier)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return CredentialBinding{}, ErrCredentialMaterial
	}
	raw, _ := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(principal, subscriptionPrincipalPrefix))
	if !ed25519.Verify(ed25519.PublicKey(key), append([]byte(subscriptionPrincipalPrefix), raw[:24]...), raw[24:]) {
		return CredentialBinding{}, fmt.Errorf("%w: attribution signature", ErrCredentialMaterial)
	}
	return binding, nil
}
