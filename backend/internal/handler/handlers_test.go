package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/security"
)

const testCredentialKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func newTestCredentialCipher(t *testing.T) *security.CredentialCipher {
	t.Helper()
	cipher, err := security.NewCredentialCipher(testCredentialKey)
	if err != nil {
		t.Fatalf("NewCredentialCipher() error = %v", err)
	}
	return cipher
}

func TestIssueTokenSignsClaimsAndSetsExpiry(t *testing.T) {
	h, err := NewHandlers(nil, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t))
	if err != nil {
		t.Fatalf("NewHandlers() error = %v", err)
	}
	before := time.Now().Add(23*time.Hour + 59*time.Minute).Unix()

	token, expiresAt, err := h.issueToken(authClaims{
		UserID:   42,
		Username: "alice",
		IsAdmin:  true,
	})
	if err != nil {
		t.Fatalf("issueToken() error = %v", err)
	}
	if expiresAt < before || expiresAt > time.Now().Add(24*time.Hour+time.Minute).Unix() {
		t.Fatalf("expiresAt = %d, want approximately 24 hours from now", expiresAt)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		t.Fatalf("token has %d parts, want 2", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if !hmac.Equal(signature, h.sign(payload)) {
		t.Fatal("token signature does not match payload")
	}

	var claims authClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	if claims.UserID != 42 || claims.Username != "alice" || !claims.IsAdmin || claims.Expiry != expiresAt {
		t.Fatalf("claims = %+v, want requested identity and generated expiry", claims)
	}
}

func TestParsePathID(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		prefix  string
		want    uint
		wantErr bool
	}{
		{name: "plain", path: "/api/v1/admin/users/12", prefix: "/api/v1/admin/users/", want: 12},
		{name: "trailing slash", path: "/api/v1/admin/users/12/", prefix: "/api/v1/admin/users/", want: 12},
		{name: "nested suffix", path: "/api/v1/admin/users/12/action", prefix: "/api/v1/admin/users/", want: 12},
		{name: "missing id", path: "/api/v1/admin/users/", prefix: "/api/v1/admin/users/", wantErr: true},
		{name: "prefix collision", path: "/api/v1/admin/users2", prefix: "/api/v1/admin/users/", wantErr: true},
		{name: "invalid id", path: "/api/v1/admin/users/nope", prefix: "/api/v1/admin/users/", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePathID(tt.path, tt.prefix)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parsePathID() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("parsePathID() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseBoolQuery(t *testing.T) {
	for _, value := range []string{"1", "true", "TRUE", " yes ", "on"} {
		if !parseBoolQuery(value) {
			t.Errorf("parseBoolQuery(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"", "0", "false", "off", "anything"} {
		if parseBoolQuery(value) {
			t.Errorf("parseBoolQuery(%q) = true, want false", value)
		}
	}
}

func TestSupportedProtocolsAreCaseInsensitive(t *testing.T) {
	h, err := NewHandlers(nil, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t))
	if err != nil {
		t.Fatalf("NewHandlers() error = %v", err)
	}
	for _, protocol := range []string{"vmess", "VLESS", "Trojan", "SHADOWSOCKS", "hysteria"} {
		if !h.isProtocolSupported(protocol) {
			t.Errorf("isProtocolSupported(%q) = false", protocol)
		}
	}
	if h.isProtocolSupported("unknown") {
		t.Fatal("isProtocolSupported(unknown) = true")
	}
}

func TestNewHandlersRejectsWeakJWTSecret(t *testing.T) {
	if _, err := NewHandlers(nil, "test-secret", newTestCredentialCipher(t)); err == nil {
		t.Fatal("NewHandlers() error = nil, want weak secret rejection")
	}
}

func TestValidateSSHHostKeyFingerprint(t *testing.T) {
	valid := "SHA256:" + base64.RawStdEncoding.EncodeToString(make([]byte, sha256.Size))
	if err := validateSSHHostKeyFingerprint(valid); err != nil {
		t.Fatalf("validateSSHHostKeyFingerprint() error = %v", err)
	}
	for _, value := range []string{"", "MD5:aa:bb", "SHA256:not-base64", "SHA256:YWJj"} {
		if err := validateSSHHostKeyFingerprint(value); err == nil {
			t.Fatalf("validateSSHHostKeyFingerprint(%q) error = nil, want rejection", value)
		}
	}
}
