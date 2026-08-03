package handler

import (
	"bytes"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	h, err := NewHandlers(nil, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "", "legacy", "")
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

func TestWantsPagedListAppliesToSelfServiceAndAdministratorRoutes(t *testing.T) {
	for _, path := range []string{
		"/api/v1/orders?paged=true&limit=25",
		"/api/v1/subscriptions?paged=true&limit=25",
		"/api/v1/traffic/records?paged=true&limit=25",
		"/api/v1/admin/orders?paged=true&limit=25",
	} {
		if !wantsPagedList(httptest.NewRequest("GET", path, nil)) {
			t.Errorf("wantsPagedList(%q) = false, want true", path)
		}
	}
	for _, path := range []string{
		"/api/v1/orders",
		"/api/v1/subscriptions?paged=false",
		"/api/v1/traffic/records?paged=1",
	} {
		if wantsPagedList(httptest.NewRequest("GET", path, nil)) {
			t.Errorf("wantsPagedList(%q) = true, want false", path)
		}
	}
}

func TestPagedDataProvidesCanonicalPageEnvelopeAndCompatibilityFields(t *testing.T) {
	data := pagedData([]int{1, 2}, 5000, 50, 25)
	page, ok := data["page"].(pageMetadata)
	if !ok {
		t.Fatalf("page = %#v, want pageMetadata", data["page"])
	}
	if page.Offset != 50 || page.Limit != 25 || page.Total != 5000 || page.NextCursor != nil {
		t.Fatalf("page = %+v, want offset 50, limit 25, total 5000", page)
	}
	if data["total"] != int64(5000) || data["offset"] != 50 || data["limit"] != 25 {
		t.Fatalf("compatibility fields = total %#v offset %#v limit %#v", data["total"], data["offset"], data["limit"])
	}
	if _, ok := data["aggregates"].(map[string]interface{}); !ok {
		t.Fatalf("aggregates = %#v, want map", data["aggregates"])
	}
	if _, ok := data["facets"].(map[string]interface{}); !ok {
		t.Fatalf("facets = %#v, want map", data["facets"])
	}
}

func TestNodeGroupSummaryDoesNotEmbedMembershipIDs(t *testing.T) {
	payload, err := json.Marshal(nodeGroupSummaryItem{
		ID:                    7,
		Name:                  "Primary",
		Code:                  "primary",
		Description:           "Primary delivery group",
		IsEnabled:             true,
		Revision:              9,
		ProtocolEndpointCount: 5000,
		PlanCount:             12,
		CreatedAt:             time.Unix(1, 0).UTC(),
		UpdatedAt:             time.Unix(2, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("marshal node-group summary: %v", err)
	}
	if !bytes.Contains(payload, []byte(`"protocol_endpoint_count":5000`)) {
		t.Fatalf("node-group summary missing endpoint count: %s", payload)
	}
	if !bytes.Contains(payload, []byte(`"revision":9`)) {
		t.Fatalf("node-group summary missing revision: %s", payload)
	}
	if bytes.Contains(payload, []byte("protocol_endpoint_ids")) {
		t.Fatalf("node-group summary embedded membership IDs: %s", payload)
	}
}

func TestPlanSummaryExposesOptimisticConcurrencyRevision(t *testing.T) {
	item := newPlanSummaryItem(model.Plan{
		ID: 11, Name: "Starter", Slug: "starter", Revision: 7,
	}, planSKUCountRow{PlanID: 11, SKUCount: 3, ActiveSKUCount: 2})
	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal plan summary: %v", err)
	}
	for _, expected := range []string{`"id":11`, `"revision":7`, `"sku_count":3`, `"active_sku_count":2`} {
		if !bytes.Contains(payload, []byte(expected)) {
			t.Fatalf("plan summary missing %s: %s", expected, payload)
		}
	}
}

func TestProtocolEndpointSelectionSnapshotSupportsScaleWithoutEmbeddingEndpointDetails(t *testing.T) {
	ids := make([]uint, 5000)
	for index := range ids {
		ids[index] = uint(index + 1)
	}
	payload, err := json.Marshal(protocolEndpointSelectionSnapshot{
		IDs:        ids,
		Total:      int64(len(ids)),
		ResolvedAt: time.Unix(10, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("marshal endpoint selection snapshot: %v", err)
	}
	if maxEndpointSelection < len(ids) {
		t.Fatalf("maxEndpointSelection = %d, must support at least %d endpoint IDs", maxEndpointSelection, len(ids))
	}
	for _, expected := range []string{`"ids":[1,2,3`, `"total":5000`, `"resolved_at":"1970-01-01T00:00:10Z"`} {
		if !bytes.Contains(payload, []byte(expected)) {
			t.Fatalf("endpoint selection snapshot missing %s", expected)
		}
	}
	for _, forbidden := range []string{"name", "address", "config", "latest_deployment", "usage"} {
		if bytes.Contains(payload, []byte(forbidden)) {
			t.Fatalf("endpoint selection snapshot exposes %q: %s", forbidden, payload)
		}
	}
}

func TestTrafficRecordSummaryOmitsRawReportPayload(t *testing.T) {
	records := []model.TrafficRecord{{
		ID: 9, UserID: 3, SubscriptionID: 4, NodeID: 5, ProtocolEndpointID: 6,
		RawBytes: 1024, ProtocolMultiplierMilli: 1500, UsedBytes: 1536,
		ReportID: "report-private", FlowID: "flow-private", EventType: "flow.completed",
		Meta: `{"source":"private"}`, UploadBytes: 400, DownloadBytes: 624,
		At: time.Unix(10, 0).UTC(),
	}}
	payload, err := json.Marshal(trafficRecordSummaries(records))
	if err != nil {
		t.Fatalf("marshal traffic summary: %v", err)
	}
	text := string(payload)
	for _, expected := range []string{
		`"id":9`, `"subscription_id":4`, `"raw_bytes":1024`,
		`"upload_bytes":400`, `"download_bytes":624`,
		`"protocol_multiplier_milli":1500`, `"used_bytes":1536`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("traffic summary %s does not contain %s", text, expected)
		}
	}
	for _, forbidden := range []string{
		"report-private", "flow-private", "flow.completed", "private",
		"report_id", "flow_id", "event_type", "meta",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("traffic summary exposes %q: %s", forbidden, text)
		}
	}
}

func TestTrafficRecordAggregatesUseStableJSONContract(t *testing.T) {
	payload, err := json.Marshal(trafficRecordAggregates{
		RawBytes: 1000, UsedBytes: 1500, UserCount: 2, SubscriptionCount: 3,
		NodeCount: 4, ProtocolEndpointCount: 5,
	})
	if err != nil {
		t.Fatalf("marshal traffic aggregates: %v", err)
	}
	for _, expected := range []string{
		`"raw_bytes":1000`, `"used_bytes":1500`, `"user_count":2`,
		`"subscription_count":3`, `"node_count":4`, `"protocol_endpoint_count":5`,
	} {
		if !strings.Contains(string(payload), expected) {
			t.Fatalf("traffic aggregates %s do not contain %s", payload, expected)
		}
	}
}

func TestFirstMissingUintIDPreservesRequestedOrder(t *testing.T) {
	if missing, ok := firstMissingUintID([]uint{9, 4, 7}, []uint{4, 9}); !ok || missing != 7 {
		t.Fatalf("firstMissingUintID() = (%d, %v), want (7, true)", missing, ok)
	}
	if missing, ok := firstMissingUintID([]uint{9, 4}, []uint{4, 9, 12}); ok || missing != 0 {
		t.Fatalf("firstMissingUintID() = (%d, %v), want (0, false)", missing, ok)
	}
	requested := make([]uint, 5000)
	existing := make([]uint, 0, 4999)
	for index := range requested {
		requested[index] = uint(index + 1)
		if requested[index] != 4096 {
			existing = append(existing, requested[index])
		}
	}
	if missing, ok := firstMissingUintID(requested, existing); !ok || missing != 4096 {
		t.Fatalf("firstMissingUintID(5000 IDs) = (%d, %v), want (4096, true)", missing, ok)
	}
}

func TestScalePageEnvelopesKeepFirstPageBounded(t *testing.T) {
	nodePage := pagedData(make([]nodeListItem, 50), 1000, 0, 50)
	endpointPage := pagedData(make([]protocolEndpointListItem, 50), 5000, 0, 50)
	if got := len(nodePage["items"].([]nodeListItem)); got != 50 || nodePage["total"] != int64(1000) {
		t.Fatalf("node page items=%d total=%v, want 50/1000", got, nodePage["total"])
	}
	if got := len(endpointPage["items"].([]protocolEndpointListItem)); got != 50 || endpointPage["total"] != int64(5000) {
		t.Fatalf("endpoint page items=%d total=%v, want 50/5000", got, endpointPage["total"])
	}
}

func TestNodeAndProtocolListItemsOmitDetailAndSecretFields(t *testing.T) {
	node := model.Node{
		ID: 1, Name: "edge", SSHHost: "root@example.invalid", SSHUser: "root", SSHPwd: "encrypted-password",
		SSHPrivateKeyPassphrase: "encrypted-passphrase", SSHPrivilegePassword: "encrypted-privilege",
		NodeCredential: "connector-secret", TrafficSecret: "traffic-secret", Config: `{"private":true}`,
	}
	nodePayload, err := json.Marshal(newNodeListItem(node, 3, time.Now().Add(-time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"config":`, `"ssh_host":`, `"ssh_user":`, `"ssh_pwd":`, "passphrase", "privilege_password", "node_credential", "traffic_secret"} {
		if strings.Contains(string(nodePayload), forbidden) {
			t.Errorf("node summary contains forbidden field %q: %s", forbidden, nodePayload)
		}
	}

	endpoint := model.ProtocolEndpoint{
		ID: 2, NodeID: 1, Name: "VLESS", Protocol: "vless", Address: "edge.example.invalid", Port: 443, PublicPort: 443,
		ServerConfig: "encrypted-server", ClientConfig: `{"id":"credential"}`, OptionalConfig: `{"private":true}`, Tags: `["internal"]`,
	}
	deployment := model.ProtocolDeployment{ID: 7, Status: "failed", Output: "private output", Error: "private error"}
	endpointPayload, err := json.Marshal(newProtocolEndpointListItem(endpoint, "edge", nil, &deployment, protocolEndpointUsage{}, true, ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"server_config", "client_config", "optional_config", "tags", "private output", "private error", `"output":`, `"error":`} {
		if strings.Contains(string(endpointPayload), forbidden) {
			t.Errorf("protocol summary contains forbidden detail %q: %s", forbidden, endpointPayload)
		}
	}
	if !strings.Contains(string(endpointPayload), `"has_error":true`) {
		t.Fatalf("protocol summary does not preserve the actionable error indicator: %s", endpointPayload)
	}
}

func TestAdminSubscriptionListItemIncludesDisplayNamesAndOmitsConfig(t *testing.T) {
	payload, err := json.Marshal(adminSubscriptionListItem{
		ID: 9, UserID: 4, UserEmail: "user@example.com", PlanID: 3,
		PlanName: "Standard", PlanSKUID: 8, SKUName: "Monthly", Status: subStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, expected := range []string{`"user_email":"user@example.com"`, `"plan_name":"Standard"`, `"sku_name":"Monthly"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("subscription summary %s does not contain %s", text, expected)
		}
	}
	if strings.Contains(text, `"config"`) {
		t.Fatalf("subscription summary leaked config: %s", text)
	}
}

func TestEffectiveSubscriptionStatusDoesNotRequirePersistence(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	tests := []struct {
		name         string
		subscription model.Subscription
		want         string
	}{
		{
			name: "active",
			subscription: model.Subscription{
				Status: subStatusActive, EndAt: now.Add(time.Hour), FlowTotal: 100, FlowUsed: 50,
			},
			want: subStatusActive,
		},
		{
			name: "expired by time",
			subscription: model.Subscription{
				Status: subStatusActive, EndAt: now, FlowTotal: 100, FlowUsed: 50,
			},
			want: subStatusExpired,
		},
		{
			name: "expired by quota",
			subscription: model.Subscription{
				Status: subStatusActive, EndAt: now.Add(time.Hour), FlowTotal: 100, FlowUsed: 100,
			},
			want: subStatusExpired,
		},
		{
			name: "canceled remains canceled",
			subscription: model.Subscription{
				Status: subStatusCanceled, EndAt: now.Add(-time.Hour), FlowTotal: 100, FlowUsed: 100,
			},
			want: subStatusCanceled,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := effectiveSubscriptionStatus(test.subscription, now); got != test.want {
				t.Fatalf("effectiveSubscriptionStatus() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSupportedProtocolsAreCaseInsensitive(t *testing.T) {
	h, err := NewHandlers(nil, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "", "legacy", "")
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

func TestProtocolKernelCapabilitiesUseConcreteZeroVersionForMieru(t *testing.T) {
	legacy, err := NewHandlers(nil, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "", "legacy", "")
	if err != nil {
		t.Fatalf("NewHandlers(legacy) error = %v", err)
	}
	if supported, reason := legacy.protocolKernelSupport("mieru"); !supported || reason != "" {
		t.Fatalf("panel Mieru support = %t %q", supported, reason)
	}
	if supported, reason := legacy.protocolKernelSupportForVersion("mieru", "0.0.15-rc.3"); supported || reason != protocolKernelMieruUnavailableReason {
		t.Fatalf("rc.3 Mieru support = %t %q", supported, reason)
	}
	if supported, reason := legacy.protocolKernelSupportForVersion("mieru", "0.0.15-rc.4"); !supported || reason != "" {
		t.Fatalf("rc.4 Mieru support = %t %q", supported, reason)
	}
	if supported, reason := legacy.protocolKernelSupport("vless"); !supported || reason != "" {
		t.Fatalf("legacy VLESS support = %t %q", supported, reason)
	}
	capabilities := legacy.protocolKernelCapabilities()
	if !capabilities["mieru"].Supported || capabilities["mieru"].MinimumZeroVersion != zeroMieruPrincipalSince {
		t.Fatalf("panel Mieru capability = %+v", capabilities["mieru"])
	}

	future, err := NewHandlers(nil, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "", "native-local", "0.0.15-rc.4")
	if err != nil {
		t.Fatalf("NewHandlers(native-local rc.4) error = %v", err)
	}
	if !future.zeroMieruAccess {
		t.Fatal("native-local rc.4 did not enable Mieru managed access")
	}

	response := httptest.NewRecorder()
	legacy.VersionHandler(response, httptest.NewRequest("GET", "/api/v1/version", nil))
	var envelope struct {
		Data struct {
			ProtocolCapabilities map[string]protocolKernelCapability `json:"protocol_capabilities"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode version response: %v", err)
	}
	if capability := envelope.Data.ProtocolCapabilities["mieru"]; !capability.Supported || capability.MinimumZeroVersion != zeroMieruPrincipalSince {
		t.Fatalf("version Mieru capability = %+v", capability)
	}
}

func TestNewHandlersRejectsWeakJWTSecret(t *testing.T) {
	if _, err := NewHandlers(nil, "test-secret", newTestCredentialCipher(t), "", "legacy", ""); err == nil {
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
		var validation *requestValidationError
		if !errors.As(err, &validation) || validation.fields["billing_unit"] == "" {
			t.Fatalf("buildPlanSKU() error = %#v, want billing_unit field", err)
		}
	}
}

func TestBuildPlanSKUReportsAllInvalidFields(t *testing.T) {
	_, err := buildPlanSKU(7, planSKUReq{SKUType: "new", BillingUnit: "month", BillingValue: 0, PriceCents: -1, SpeedLimitMbps: -1})
	var validation *requestValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("buildPlanSKU() error = %#v, want requestValidationError", err)
	}
	for _, field := range []string{"code", "name", "currency", "billing_value", "price_cents", "traffic_bytes", "device_limit", "speed_limit_mbps"} {
		if validation.fields[field] == "" {
			t.Errorf("missing field error for %q: %#v", field, validation.fields)
		}
	}
	prefixed := prefixValidationError(err, "skus.0.")
	if !errors.As(prefixed, &validation) || validation.fields["skus.0.price_cents"] == "" {
		t.Fatalf("prefixValidationError() = %#v", prefixed)
	}
}

func TestValidateNodeProtocolConfigsReportsBothJSONFields(t *testing.T) {
	err := validateNodeProtocolConfigs("vless", "[]", "not-json")
	var validation *requestValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("validateNodeProtocolConfigs() error = %#v, want requestValidationError", err)
	}
	for _, field := range []string{"config", "client_config"} {
		if validation.fields[field] == "" {
			t.Errorf("missing field error for %q: %#v", field, validation.fields)
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
	if got := billedTrafficBytes(1, 1000); got != 1 {
		t.Fatalf("one-byte latency probe = %d, want 1", got)
	}
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

func TestValidateSSHFieldsReportsSpecificFields(t *testing.T) {
	err := validateSSHFields("", 70000, "", sshAuthPrivateKey, "", "")
	var validation *requestValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("validateSSHFields() error = %#v, want requestValidationError", err)
	}
	for _, field := range []string{"ssh_host", "ssh_port", "ssh_user", "ssh_private_key"} {
		if validation.fields[field] == "" {
			t.Errorf("missing field error for %q: %#v", field, validation.fields)
		}
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

func TestBusinessListFilterEnums(t *testing.T) {
	h := &handlers{}
	for _, test := range []struct {
		status     string
		adminScope bool
		want       []string
		valid      bool
	}{
		{status: adminAttentionStatus, adminScope: true, want: []string{orderStatusPending, orderStatusFailed}, valid: true},
		{status: orderStatusPaid, adminScope: true, want: []string{orderStatusPaid}, valid: true},
		{status: adminAttentionStatus, adminScope: false, valid: false},
		{status: "processing", adminScope: true, valid: false},
	} {
		got, valid := h.orderListStatusValues(test.status, test.adminScope)
		if valid != test.valid || len(got) != len(test.want) {
			t.Errorf("orderListStatusValues(%q, %v) = %#v, %v; want %#v, %v", test.status, test.adminScope, got, valid, test.want, test.valid)
			continue
		}
		for index := range got {
			if got[index] != test.want[index] {
				t.Errorf("orderListStatusValues(%q, %v)[%d] = %q, want %q", test.status, test.adminScope, index, got[index], test.want[index])
			}
		}
	}
	for _, value := range []string{"new", "renewal", "upgrade", "traffic_pack"} {
		if !isValidOrderType(value) {
			t.Errorf("isValidOrderType(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"", "trial", "new "} {
		if isValidOrderType(value) {
			t.Errorf("isValidOrderType(%q) = true, want false", value)
		}
	}
	for _, value := range []string{subStatusActive, subStatusExpired, subStatusCanceled} {
		if !isValidSubscriptionStatus(value) {
			t.Errorf("isValidSubscriptionStatus(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"", "pending", "active "} {
		if isValidSubscriptionStatus(value) {
			t.Errorf("isValidSubscriptionStatus(%q) = true, want false", value)
		}
	}
	for _, value := range []string{"available", "exhausted"} {
		if !isValidSubscriptionQuotaFilter(value) {
			t.Errorf("isValidSubscriptionQuotaFilter(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"", "low", "available "} {
		if isValidSubscriptionQuotaFilter(value) {
			t.Errorf("isValidSubscriptionQuotaFilter(%q) = true, want false", value)
		}
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

func TestTrafficReconciliationAggregatesJSONContract(t *testing.T) {
	payload, err := json.Marshal(trafficReconciliationAggregates{
		SubscriptionCount:   3,
		MatchedCount:        1,
		MissingRecordsCount: 1,
		OverRecordedCount:   1,
		FlowUsed:            11,
		RecordedBytes:       13,
		MissingBytes:        4,
		OverRecordedBytes:   6,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		`"subscription_count":3`,
		`"matched_count":1`,
		`"missing_records_count":1`,
		`"over_recorded_count":1`,
		`"flow_used":11`,
		`"recorded_bytes":13`,
		`"missing_bytes":4`,
		`"over_recorded_bytes":6`,
	} {
		if !bytes.Contains(payload, []byte(field)) {
			t.Errorf("traffic reconciliation aggregate payload %s is missing %s", payload, field)
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

func TestNormalizePlanPolicyReportsSpecificFields(t *testing.T) {
	_, err := normalizePlanPolicy(planCreateReq{FamilyLimit: -1, ResetPolicy: 9, TrafficCalcMode: 9}, planSKUReq{})
	var validation *requestValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("normalizePlanPolicy() error = %#v, want requestValidationError", err)
	}
	for _, field := range []string{"traffic_bytes", "device_limit", "family_limit", "reset_policy", "traffic_calc_mode"} {
		if validation.fields[field] == "" {
			t.Errorf("missing field error for %q: %#v", field, validation.fields)
		}
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
	h, err := NewHandlers(nil, "0123456789abcdef0123456789abcdef", cipher, "", "legacy", "")
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

func TestParseZeroFlowProjectionPrefersRecordAndAuth(t *testing.T) {
	event := zeroEventEnvelope{
		SchemaID:     "zero.event.v1",
		EventID:      "flow.updated:42:2",
		EventType:    "flow.updated",
		PrincipalKey: "envelope-principal",
		Payload: json.RawMessage(`{
			"flow_id":"legacy-flow",
			"record":{
				"flow_id":"42",
				"revision":2,
				"auth":{"principal_key":"subscription:7:endpoint:3"},
				"traffic":{"bytes_up":1024,"bytes_down":4096}
			}
		}`),
	}
	projection, err := parseZeroFlowProjection(event)
	if err != nil {
		t.Fatal(err)
	}
	if projection.FlowID != "42" || projection.Revision != 2 || projection.PrincipalKey != "subscription:7:endpoint:3" || projection.BytesUp != 1024 || projection.BytesDown != 4096 {
		t.Fatalf("unexpected projection: %+v", projection)
	}
}

func TestParseZeroFlowProjectionPreservesLargeTrafficCounters(t *testing.T) {
	event := zeroEventEnvelope{
		Payload: json.RawMessage(`{
			"record":{
				"flow_id":"large-counter",
				"revision":9007199254740993,
				"traffic":{"bytes_up":9007199254740993,"bytes_down":17}
			}
		}`),
	}
	projection, err := parseZeroFlowProjection(event)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Revision != 9007199254740993 || projection.BytesUp != 9007199254740993 || projection.BytesDown != 17 {
		t.Fatalf("large counters lost precision: %+v", projection)
	}
}

func TestParseZeroStatsProjectionPreservesConnectorCounters(t *testing.T) {
	stats, err := parseZeroStatsProjection(json.RawMessage(`{
		"active_sessions":42,
		"bytes_up":9007199254740993,
		"bytes_down":17
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if stats.ActiveSessions != 42 || stats.BytesUp != 9007199254740993 || stats.BytesDown != 17 {
		t.Fatalf("unexpected stats projection: %+v", stats)
	}
}

func TestManagedAccessUserFieldsMapsSafeSubscriptionPolicy(t *testing.T) {
	now := time.UnixMilli(1_753_500_000_123).UTC()
	context := runtimeCredentialContext{
		Credential: model.ProtocolCredential{PrincipalKey: "subscription:7:endpoint:3"},
		Subscription: model.Subscription{
			ID: 7, FlowTotal: 10_000, FlowUsed: 2_500, SpeedLimitMbps: 80,
			DeviceLimit: 3, TrafficCalcMode: trafficCalcBoth, UpdatedAt: now,
		},
		SoleActiveCredential: true,
	}
	user, err := managedAccessUserFields(context)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]interface{}{
		"principal_key":   "subscription:7:endpoint:3",
		"policy_revision": uint64(now.UnixMilli()),
		"up_bps":          uint64(10_000_000),
		"down_bps":        uint64(10_000_000),
		"device_limit":    uint32(3),
	} {
		if got := user[key]; got != want {
			t.Errorf("%s = %#v, want %#v", key, got, want)
		}
	}
}

func TestProtocolCredentialContractIsExplicitlyStaged(t *testing.T) {
	legacy := &handlers{}
	native := &handlers{zeroNativeAccess: true}
	mieruNative := &handlers{zeroNativeAccess: true, zeroMieruAccess: true}
	if legacy.protocolUsesSubscriptionCredential("trojan") {
		t.Fatal("legacy contract unexpectedly enabled Trojan managed users")
	}
	if native.protocolUsesSubscriptionCredential("mieru") {
		t.Fatal("native-local contract unexpectedly enabled unsupported Mieru principals")
	}
	if !native.protocolUsesSubscriptionCredential("trojan") || !native.protocolUsesSubscriptionCredential("hysteria2") {
		t.Fatal("native-local contract did not enable all managed-password protocols")
	}
	if legacy.protocolStoresSubscriptionCredential("mieru") {
		t.Fatal("legacy contract prepared credentials for a panel-disabled protocol")
	}
	if !mieruNative.protocolStoresSubscriptionCredential("mieru") {
		t.Fatal("native-local-mieru contract did not enable Mieru credential storage")
	}
	if !mieruNative.protocolUsesSubscriptionCredential("mieru") {
		t.Fatal("native-local-mieru contract did not enable Mieru managed users")
	}
	if mieruNative.endpointDeliversSubscriptionCredential(model.ProtocolEndpoint{Protocol: "mieru"}) {
		t.Fatal("Mieru subscription delivery switched before a successful node publication")
	}
	if !mieruNative.endpointDeliversSubscriptionCredential(model.ProtocolEndpoint{Protocol: "mieru", MieruPrincipalReady: true}) {
		t.Fatal("Mieru subscription delivery did not switch after publication readiness")
	}
	for _, protocol := range []string{"trojan", "hysteria2"} {
		if native.endpointDeliversSubscriptionCredential(model.ProtocolEndpoint{Protocol: protocol}) {
			t.Fatalf("%s subscription delivery switched before a successful node publication", protocol)
		}
		if !legacy.endpointDeliversSubscriptionCredential(model.ProtocolEndpoint{Protocol: protocol, ManagedPrincipalReady: true}) {
			t.Fatalf("%s subscription delivery ignored the actual node publication readiness", protocol)
		}
		if endpointUsesRuntimeCredentials(protocol, false, false) {
			t.Fatalf("legacy runtime attempted duplicate %s credential inbounds on one port", protocol)
		}
		if !endpointUsesRuntimeCredentials(protocol, true, false) {
			t.Fatalf("native runtime did not compile %s managed users", protocol)
		}
	}
	if legacy.desiredProtocolCredentialStatus(model.ProtocolEndpoint{Protocol: "mieru"}) != protocolCredentialStatusPrepared {
		t.Fatal("legacy Mieru credential was not kept in prepared state")
	}
	if legacy.desiredProtocolCredentialStatus(model.ProtocolEndpoint{Protocol: "mieru", MieruPrincipalReady: true}) != protocolCredentialStatusActive {
		t.Fatal("Mieru credential was deactivated before a reverse publication completed")
	}
	if mieruNative.desiredProtocolCredentialStatus(model.ProtocolEndpoint{Protocol: "mieru"}) != protocolCredentialStatusActive {
		t.Fatal("Mieru native rollout did not activate prepared credentials for compilation")
	}
}

func TestManagedAccessUserFieldsOmitsUnsafeDistributedQuota(t *testing.T) {
	context := runtimeCredentialContext{
		Credential: model.ProtocolCredential{PrincipalKey: "subscription:7:endpoint:3"},
		Subscription: model.Subscription{
			ID: 7, FlowTotal: 10_000, FlowUsed: 2_500, DeviceLimit: 2,
			TrafficCalcMode: trafficCalcUpload,
		},
		SoleActiveCredential: false,
	}
	user, err := managedAccessUserFields(context)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"up_bps", "down_bps", "device_limit", "quota_remaining_bytes"} {
		if _, exists := user[field]; exists {
			t.Fatalf("unsafe distributed policy field %s was projected: %#v", field, user)
		}
	}
}

func TestZeroConfigPublishScriptValidatesSwitchesAndChecksHealth(t *testing.T) {
	script := buildZeroConfigPublishScript("/tmp/stage", strings.Repeat("a", 64), 91)
	for _, required := range []string{
		`zero validate "$stage/runtime.json"`,
		`mv -Tf /etc/zerodenet/current.json.next /etc/zerodenet/current.json`,
		`systemctl restart zero`,
		`zero status --json --socket`,
		`rollback()`,
		`$backup/old_link`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("publish script is missing %q", required)
		}
	}
	rollback := buildZeroConfigRollbackScript(91)
	if !strings.Contains(rollback, "config-91.json") || !strings.Contains(rollback, "systemctl restart zero") {
		t.Fatalf("unexpected rollback script: %s", rollback)
	}
}

func TestShadowsocks2022CredentialUsesCipherKeyLength(t *testing.T) {
	cipher := newTestCredentialCipher(t)
	h, err := NewHandlers(nil, "0123456789abcdef0123456789abcdef", cipher, "", "legacy", "")
	if err != nil {
		t.Fatal(err)
	}
	template, err := cipher.Encrypt(`{"type":"shadowsocks","cipher":"2022-blake3-aes-128-gcm","password":"placeholder"}`)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := h.newProtocolCredentialSecret(model.ProtocolEndpoint{Protocol: "shadowsocks", ServerConfig: template})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		t.Fatalf("credential is not standard base64: %v", err)
	}
	if len(decoded) != 16 {
		t.Fatalf("decoded credential length = %d, want 16", len(decoded))
	}
}
