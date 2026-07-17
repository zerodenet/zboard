package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

func TestNewNodeReportSecret(t *testing.T) {
	secret, prefix, err := newNodeReportSecret()
	if err != nil {
		t.Fatalf("newNodeReportSecret() error = %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("decoded secret length = %d, want 32", len(decoded))
	}
	if prefix != secret[:12] {
		t.Fatalf("prefix = %q, want first 12 secret characters", prefix)
	}
}

func TestNodeReportSignature(t *testing.T) {
	body := []byte(`{"report_id":"report-001","user_id":42,"used_bytes":1024}`)
	signature := nodeReportSignature("node-secret", "7", "1721188800", "nonce-0123456789", body)
	if len(signature) != sha256.Size {
		t.Fatalf("signature length = %d, want %d", len(signature), sha256.Size)
	}
	if got := hex.EncodeToString(signature); got != "a91d3d7a3621bd205e5c3f0f7fd4612eaa8322f599b2b9ee501861d3014a26ee" {
		t.Fatalf("signature = %s, want stable canonical signature", got)
	}
	if hmac.Equal(signature, nodeReportSignature("node-secret", "7", "1721188800", "nonce-0123456780", body)) {
		t.Fatal("signature did not change when nonce changed")
	}
	if hmac.Equal(signature, nodeReportSignature("node-secret", "7", "1721188800", "nonce-0123456789", append(body, '\n'))) {
		t.Fatal("signature did not change when body changed")
	}
}

func TestValidateNodeReportTimestamp(t *testing.T) {
	now := time.Unix(1721188800, 0).UTC()
	for _, value := range []string{"1721188500", "1721188800", "1721189100"} {
		if _, err := validateNodeReportTimestamp(value, now); err != nil {
			t.Errorf("validateNodeReportTimestamp(%q) error = %v", value, err)
		}
	}
	for _, value := range []string{"", "01721188800", "1721188499", "1721189101", "not-a-time"} {
		if _, err := validateNodeReportTimestamp(value, now); err == nil {
			t.Errorf("validateNodeReportTimestamp(%q) error = nil, want rejection", value)
		}
	}
}

func TestValidNodeReportIdentifier(t *testing.T) {
	for _, value := range []string{"report-001", "nonce_0123456789", "uuid:1234.5678"} {
		if !validNodeReportIdentifier(value, 8, 64) {
			t.Errorf("validNodeReportIdentifier(%q) = false", value)
		}
	}
	for _, value := range []string{"short", "contains space", "contains/slash", strings.Repeat("a", 65)} {
		if validNodeReportIdentifier(value, 8, 64) {
			t.Errorf("validNodeReportIdentifier(%q) = true, want rejection", value)
		}
	}
}

func TestOrderTransitionAllowed(t *testing.T) {
	tests := []struct {
		name    string
		current string
		target  string
		force   bool
		want    bool
	}{
		{name: "pending to paid", current: orderStatusPending, target: orderStatusPaid, want: true},
		{name: "pending to failed", current: orderStatusPending, target: orderStatusFailed, want: true},
		{name: "pending to canceled", current: orderStatusPending, target: orderStatusCanceled, want: true},
		{name: "paid idempotent", current: orderStatusPaid, target: orderStatusPaid, want: true},
		{name: "paid cannot fail", current: orderStatusPaid, target: orderStatusFailed, want: false},
		{name: "paid cannot cancel with force", current: orderStatusPaid, target: orderStatusCanceled, force: true, want: false},
		{name: "failed can settle", current: orderStatusFailed, target: orderStatusPaid, want: true},
		{name: "failed cannot cancel normally", current: orderStatusFailed, target: orderStatusCanceled, want: false},
		{name: "failed can cancel by admin reconciliation", current: orderStatusFailed, target: orderStatusCanceled, force: true, want: true},
		{name: "canceled cannot settle normally", current: orderStatusCanceled, target: orderStatusPaid, want: false},
		{name: "canceled can settle by admin reconciliation", current: orderStatusCanceled, target: orderStatusPaid, force: true, want: true},
		{name: "unknown target", current: orderStatusPending, target: "refunded", force: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := orderTransitionAllowed(tt.current, tt.target, tt.force); got != tt.want {
				t.Fatalf("orderTransitionAllowed(%q, %q, %v) = %v, want %v", tt.current, tt.target, tt.force, got, tt.want)
			}
		})
	}
}

func TestValidateNodeProtocolConfigs(t *testing.T) {
	if err := validateNodeProtocolConfigs(`{"listen":"0.0.0.0:443"}`, `{"server":"node.example.com","port":443}`); err != nil {
		t.Fatalf("validateNodeProtocolConfigs() error = %v", err)
	}
	for _, test := range []struct {
		name   string
		server string
		client string
	}{
		{name: "missing server", client: `{}`},
		{name: "invalid server", server: `{`, client: `{}`},
		{name: "missing client", server: `{}`},
		{name: "invalid client", server: `{}`, client: `{`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateNodeProtocolConfigs(test.server, test.client); err == nil {
				t.Fatal("validateNodeProtocolConfigs() error = nil, want rejection")
			}
		})
	}
}
