package handler

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/security"
	"golang.org/x/crypto/ssh"
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
	h, err := NewHandlers(nil, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "")
	if err != nil {
		t.Fatalf("NewHandlers() error = %v", err)
	}
	before := time.Now().Add(23*time.Hour + 59*time.Minute).Unix()

	token, expiresAt, err := h.issueToken(authClaims{
		UserID:  42,
		Email:   "alice@example.com",
		IsAdmin: true,
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
	if claims.UserID != 42 || claims.Email != "alice@example.com" || !claims.IsAdmin || claims.Expiry != expiresAt {
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
	h, err := NewHandlers(nil, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "")
	if err != nil {
		t.Fatalf("NewHandlers() error = %v", err)
	}
	for _, protocol := range []string{"vmess", "VLESS", "Trojan", "SHADOWSOCKS", "hysteria2", "Mieru"} {
		if !h.isProtocolSupported(protocol) {
			t.Errorf("isProtocolSupported(%q) = false", protocol)
		}
	}
	if h.isProtocolSupported("unknown") {
		t.Fatal("isProtocolSupported(unknown) = true")
	}
}

func TestNewHandlersRejectsWeakJWTSecret(t *testing.T) {
	if _, err := NewHandlers(nil, "test-secret", newTestCredentialCipher(t), ""); err == nil {
		t.Fatal("NewHandlers() error = nil, want weak secret rejection")
	}
}

func TestValidateSetupRequestNormalizesValues(t *testing.T) {
	body := setupRequest{
		SiteName:      "  Example Panel  ",
		SiteURL:       "https://panel.example.com/",
		AdminEmail:    " operator@example.com ",
		AdminPassword: "strong-admin-password",
	}
	if err := validateSetupRequest(&body); err != nil {
		t.Fatalf("validateSetupRequest() error = %v", err)
	}
	if body.SiteName != "Example Panel" || body.SiteURL != "https://panel.example.com" || body.AdminEmail != "operator@example.com" {
		t.Fatalf("normalized setup request = %+v", body)
	}
}

func TestValidateSetupRequestRejectsUnsafeValues(t *testing.T) {
	valid := setupRequest{
		SiteName:      "Example Panel",
		SiteURL:       "https://panel.example.com",
		AdminEmail:    "operator@example.com",
		AdminPassword: "strong-admin-password",
	}
	tests := []struct {
		name string
		edit func(*setupRequest)
	}{
		{name: "relative URL", edit: func(r *setupRequest) { r.SiteURL = "/panel" }},
		{name: "URL credentials", edit: func(r *setupRequest) { r.SiteURL = "https://user:pass@example.com" }},
		{name: "invalid email", edit: func(r *setupRequest) { r.AdminEmail = "operator" }},
		{name: "short password", edit: func(r *setupRequest) { r.AdminPassword = "short" }},
		{name: "bcrypt overflow", edit: func(r *setupRequest) { r.AdminPassword = strings.Repeat("x", 73) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := valid
			tt.edit(&body)
			if err := validateSetupRequest(&body); err == nil {
				t.Fatal("validateSetupRequest() error = nil, want rejection")
			}
		})
	}
}

func TestBuildPlanSKUValidatesCommercialSpecification(t *testing.T) {
	sku, err := buildPlanSKU(7, planSKUReq{
		Code: " Pro-Monthly ", Name: "Monthly", BillingUnit: "month", BillingValue: 1,
		PriceCents: 1999, Currency: "cny", TrafficBytes: 100 << 30, DeviceLimit: 5,
	})
	if err != nil {
		t.Fatalf("buildPlanSKU() error = %v", err)
	}
	if sku.PlanID != 7 || sku.Code != "pro-monthly" || sku.Currency != "CNY" || !sku.IsActive {
		t.Fatalf("buildPlanSKU() = %+v", sku)
	}
	for _, unit := range []string{"", "week", "one_time"} {
		_, err := buildPlanSKU(7, planSKUReq{Code: "x", Name: "x", BillingUnit: unit, BillingValue: 1, Currency: "CNY", TrafficBytes: 1, DeviceLimit: 1})
		if err == nil {
			t.Fatalf("buildPlanSKU() accepted billing unit %q", unit)
		}
	}
}

func TestAddBillingPeriodClampsCalendarEnd(t *testing.T) {
	base := time.Date(2024, time.January, 31, 12, 30, 0, 0, time.UTC)
	monthly, err := addBillingPeriod(base, "month", 1)
	if err != nil || !monthly.Equal(time.Date(2024, time.February, 29, 12, 30, 0, 0, time.UTC)) {
		t.Fatalf("monthly = %v, err = %v", monthly, err)
	}
	yearly, err := addBillingPeriod(monthly, "year", 1)
	if err != nil || !yearly.Equal(time.Date(2025, time.February, 28, 12, 30, 0, 0, time.UTC)) {
		t.Fatalf("yearly = %v, err = %v", yearly, err)
	}
}

func TestBilledTrafficBytesRoundsUp(t *testing.T) {
	if got := billedTrafficBytes(1001, 1500); got != 1502 {
		t.Fatalf("billedTrafficBytes() = %d, want 1502", got)
	}
	if got := billedTrafficBytes(1024, 500); got != 512 {
		t.Fatalf("billedTrafficBytes() = %d, want 512", got)
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

func TestValidateSSHFieldsAllowsAutomaticHostKeyEnrollment(t *testing.T) {
	validFingerprint := "SHA256:" + base64.RawStdEncoding.EncodeToString(make([]byte, sha256.Size))
	if err := validateSSHFields("node.example.com", 22, "root", sshAuthPassword, "secret", ""); err != nil {
		t.Fatalf("password authentication before host-key enrollment should be valid: %v", err)
	}
	if err := validateSSHFields("node.example.com", 22, "root", sshAuthPrivateKey, "private-key", validFingerprint); err != nil {
		t.Fatalf("private-key authentication with a recorded host key should be valid: %v", err)
	}
	if err := validateSSHFields("node.example.com", 22, "root", "fingerprint", "secret", ""); err == nil {
		t.Fatal("host fingerprint was accepted as an authentication method")
	}
	if err := validateSSHFields("node.example.com", 22, "root", sshAuthPassword, "secret", "SHA256:invalid"); err == nil {
		t.Fatal("an invalid recorded host key was accepted")
	}
}

func TestVerifiedHostKeyCallbackEnrollsThenChecksRecordedFingerprint(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := ssh.NewPublicKey(privateKey.Public())
	if err != nil {
		t.Fatal(err)
	}
	want := ssh.FingerprintSHA256(publicKey)
	observed := ""
	if err := verifiedHostKeyCallback("", &observed)("node.example.com", nil, publicKey); err != nil {
		t.Fatalf("first host-key enrollment was rejected: %v", err)
	}
	if observed != want {
		t.Fatalf("observed fingerprint = %q, want %q", observed, want)
	}
	if err := verifiedHostKeyCallback(want, nil)("node.example.com", nil, publicKey); err != nil {
		t.Fatalf("verification rejected the recorded key: %v", err)
	}
	if err := verifiedHostKeyCallback("SHA256:"+base64.RawStdEncoding.EncodeToString(make([]byte, sha256.Size)), nil)("node.example.com", nil, publicKey); err == nil {
		t.Fatal("verification accepted a different recorded key")
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

func TestExtractBearerTokenRequiresConnectorScheme(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/v1/nodes/7/commands", nil)
	request.Header.Set("Authorization", "Bearer connector-secret")
	token, err := extractBearerToken(request)
	if err != nil || token != "connector-secret" {
		t.Fatalf("extractBearerToken() = %q, %v", token, err)
	}
	for _, value := range []string{"", "Basic connector-secret", "Bearer", "Bearer two tokens"} {
		request.Header.Set("Authorization", value)
		if _, err := extractBearerToken(request); err == nil {
			t.Fatalf("extractBearerToken(%q) error = nil, want rejection", value)
		}
	}
}

func TestDecodeNodeConnectorHeartbeatUsesZeroContract(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/v1/nodes/7/heartbeat", strings.NewReader(`{
		"node_id":"7","build_id":"zero-1.0","uptime_seconds":3600,
		"active_flows":42,"bytes_up":1024,"bytes_down":4096,"future_field":true
	}`))
	heartbeat, err := decodeNodeConnectorHeartbeat(request)
	if err != nil {
		t.Fatalf("decodeNodeConnectorHeartbeat() error = %v", err)
	}
	if heartbeat.NodeID != "7" || heartbeat.BuildID != "zero-1.0" || heartbeat.UptimeSeconds != 3600 || heartbeat.ActiveFlows != 42 || heartbeat.BytesUp != 1024 || heartbeat.BytesDown != 4096 {
		t.Fatalf("heartbeat = %+v", heartbeat)
	}

	request = httptest.NewRequest("POST", "/api/v1/nodes/7/heartbeat", strings.NewReader(`{"build_id":"zero-1.0"}`))
	if _, err := decodeNodeConnectorHeartbeat(request); err == nil {
		t.Fatal("missing node_id should be rejected")
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
	if err := validateNodeProtocolConfigs("vless", `{"type":"vless","users":[]}`, `{"server":"node.example.com","port":443}`); err != nil {
		t.Fatalf("validateNodeProtocolConfigs() error = %v", err)
	}
	for _, test := range []struct {
		name   string
		server string
		client string
	}{
		{name: "missing server", client: `{}`},
		{name: "invalid server", server: `{`, client: `{}`},
		{name: "server array", server: `[]`, client: `{}`},
		{name: "missing protocol type", server: `{}`, client: `{}`},
		{name: "mismatched protocol type", server: `{"type":"trojan"}`, client: `{}`},
		{name: "missing client", server: `{"type":"vless"}`},
		{name: "invalid client", server: `{"type":"vless"}`, client: `{`},
		{name: "client array", server: `{"type":"vless"}`, client: `[]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateNodeProtocolConfigs("vless", test.server, test.client); err == nil {
				t.Fatal("validateNodeProtocolConfigs() error = nil, want rejection")
			}
		})
	}
}

func TestParsePagination(t *testing.T) {
	tests := []struct {
		name       string
		offset     string
		limit      string
		wantOffset int
		wantLimit  int
		wantErr    bool
	}{
		{name: "defaults", wantOffset: 0, wantLimit: 50},
		{name: "explicit", offset: "20", limit: "100", wantOffset: 20, wantLimit: 100},
		{name: "trimmed", offset: " 1 ", limit: " 200 ", wantOffset: 1, wantLimit: 200},
		{name: "negative offset", offset: "-1", wantErr: true},
		{name: "invalid offset", offset: "nope", wantErr: true},
		{name: "zero limit", limit: "0", wantErr: true},
		{name: "limit too large", limit: "201", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			offset, limit, err := parsePagination(tt.offset, tt.limit)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parsePagination() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && (offset != tt.wantOffset || limit != tt.wantLimit) {
				t.Fatalf("parsePagination() = (%d, %d), want (%d, %d)", offset, limit, tt.wantOffset, tt.wantLimit)
			}
		})
	}
}

func TestTrafficReconciliationResult(t *testing.T) {
	tests := map[int64]string{
		0:    "matched",
		1:    "missing_records",
		1024: "missing_records",
		-1:   "over_recorded",
	}
	for difference, want := range tests {
		if got := trafficReconciliationResult(difference); got != want {
			t.Errorf("trafficReconciliationResult(%d) = %q, want %q", difference, got, want)
		}
	}
}

func TestNormalizePlanPolicyUsesSKUDefaults(t *testing.T) {
	policy, err := normalizePlanPolicy(planCreateReq{}, planSKUReq{
		TrafficBytes: 1024, DeviceLimit: 3, SpeedLimitMbps: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if policy.TrafficBytes != 1024 || policy.DeviceLimit != 3 || policy.SpeedLimitMbps != 20 {
		t.Fatalf("unexpected policy: %#v", policy)
	}
	if !policy.IsRenewable {
		t.Fatalf("unexpected policy defaults: %#v", policy)
	}
}

func TestTrafficAccountingUsesDirectionAndEndpointMultiplier(t *testing.T) {
	if got := trafficBytesForMode(100, 300, trafficCalcBoth); got != 400 {
		t.Fatalf("both directions = %d, want 400", got)
	}
	if got := trafficBytesForMode(100, 300, trafficCalcUpload); got != 100 {
		t.Fatalf("upload = %d, want 100", got)
	}
	if got := trafficBytesForMode(100, 300, trafficCalcDownload); got != 300 {
		t.Fatalf("download = %d, want 300", got)
	}
	billed, err := billedTrafficBytesChecked(1000, 1500)
	if err != nil {
		t.Fatal(err)
	}
	if billed != 1500 {
		t.Fatalf("billed = %d, want 1500", billed)
	}
}

func TestNextTrafficReset(t *testing.T) {
	base := time.Date(2026, time.January, 31, 12, 0, 0, 0, time.UTC)
	monthly := nextTrafficReset(base, 2)
	if monthly == nil || monthly.Year() != 2026 || monthly.Month() != time.February || monthly.Day() != 28 {
		t.Fatalf("monthly reset = %v", monthly)
	}
	firstOfMonth := nextTrafficReset(base, 1)
	if firstOfMonth == nil || firstOfMonth.Month() != time.February || firstOfMonth.Day() != 1 {
		t.Fatalf("calendar reset = %v", firstOfMonth)
	}
	if got := nextTrafficReset(base, 5); got != nil {
		t.Fatalf("no-reset policy = %v", got)
	}
}

func TestValidateSSHPrivilege(t *testing.T) {
	for _, test := range []struct {
		mode     string
		password string
		wantErr  bool
	}{
		{mode: "none"},
		{mode: "sudo"},
		{mode: "sudo", password: "sudo-secret"},
		{mode: "su", password: "root-secret"},
		{mode: "su", wantErr: true},
		{mode: "doas", wantErr: true},
	} {
		if err := validateSSHPrivilege(test.mode, test.password); (err != nil) != test.wantErr {
			t.Fatalf("validateSSHPrivilege(%q) error = %v, wantErr %t", test.mode, err, test.wantErr)
		}
	}
}

func TestPrepareSSHCommandDoesNotExposePrivilegePassword(t *testing.T) {
	cipher := newTestCredentialCipher(t)
	h, err := NewHandlers(nil, "0123456789abcdef0123456789abcdef", cipher, "")
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := cipher.Encrypt("root-secret")
	if err != nil {
		t.Fatal(err)
	}
	command, stdin, pty, err := h.prepareSSHCommand(model.Node{SSHPrivilegeMode: "su", SSHPrivilegePassword: encrypted}, "id -u", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(command, "su root -c ") || strings.Contains(command, "root-secret") || stdin != "root-secret\n" || !pty {
		t.Fatalf("unexpected su command=%q stdin=%q pty=%t", command, stdin, pty)
	}
	command, stdin, pty, err = h.prepareSSHCommand(model.Node{SSHPrivilegeMode: "sudo"}, "id -u", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(command, "sudo -n -- sh -c ") || stdin != "" || pty {
		t.Fatalf("unexpected passwordless sudo command=%q stdin=%q pty=%t", command, stdin, pty)
	}
}
