package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestPrepareMieruEndpointConfigsGeneratesAndHidesCredential(t *testing.T) {
	h, err := NewHandlers(nil, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "", "legacy", "")
	if err != nil {
		t.Fatalf("NewHandlers() error = %v", err)
	}

	serverRaw, clientRaw, err := h.prepareMieruEndpointConfigs(
		0,
		`{"type":"mieru","users":[]}`,
		`{"type":"mieru","username":"admin-value","password":"admin-value","transport":"tcp"}`,
	)
	if err != nil {
		t.Fatalf("prepareMieruEndpointConfigs() error = %v", err)
	}
	var server map[string]interface{}
	if err := json.Unmarshal([]byte(serverRaw), &server); err != nil {
		t.Fatalf("decode server config: %v", err)
	}
	password := mieruEndpointPassword(server)
	if len(password) < 40 {
		t.Fatalf("generated Mieru password length = %d, want a high-entropy value", len(password))
	}
	if strings.Contains(clientRaw, password) || strings.Contains(clientRaw, "admin-value") {
		t.Fatalf("stored client config contains a credential: %s", clientRaw)
	}

	ciphertext, err := h.credentialCipher.Encrypt(serverRaw)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	subscriptionConfig, err := h.endpointSubscriptionClientConfig(model.ProtocolEndpoint{
		Protocol:     "mieru",
		ServerConfig: ciphertext,
		ClientConfig: clientRaw,
	})
	if err != nil {
		t.Fatalf("endpointSubscriptionClientConfig() error = %v", err)
	}
	var subscription map[string]interface{}
	if err := json.Unmarshal(subscriptionConfig, &subscription); err != nil {
		t.Fatalf("decode subscription config: %v", err)
	}
	if subscription["password"] != password {
		t.Fatal("subscription config did not receive the generated endpoint credential")
	}
	if _, exists := subscription["username"]; exists {
		t.Fatal("Zero Mieru subscription config should rely on username=password compatibility")
	}

	redactedServer, redactedClient := redactMieruEndpointAdminConfigs(serverRaw, string(subscriptionConfig))
	if strings.Contains(redactedServer, password) || strings.Contains(redactedClient, password) {
		t.Fatal("admin Mieru config redaction leaked the endpoint credential")
	}
}

func TestValidateGeneratedZeroDocumentRejectsIncompleteMieru(t *testing.T) {
	document := map[string]interface{}{
		"outbounds": []interface{}{
			map[string]interface{}{
				"tag":      "mieru",
				"protocol": map[string]interface{}{"type": "mieru"},
			},
		},
		"outbound_groups": []interface{}{},
	}
	if err := validateGeneratedZeroDocument(document); err == nil || !strings.Contains(err.Error(), "server") {
		t.Fatalf("validateGeneratedZeroDocument() error = %v, want missing server", err)
	}
}

func TestClashMieruDefaultsUsernameToGeneratedPassword(t *testing.T) {
	proxy, err := clashProxy(subscriptionTemplateEndpoint{
		ID:         9,
		Name:       "Mieru",
		Address:    "mieru.example.com",
		PublicPort: 2999,
		Protocol:   "mieru",
		Config: map[string]interface{}{
			"type": "mieru", "password": "generated-secret",
		},
	})
	if err != nil {
		t.Fatalf("clashProxy() error = %v", err)
	}
	if proxy["username"] != "generated-secret" || proxy["password"] != "generated-secret" {
		t.Fatalf("Clash Mieru credential mapping = %#v", proxy)
	}
}

func TestNewMieruSubscriptionCredentialIsIndependent(t *testing.T) {
	h, err := NewHandlers(nil, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "", "native-local-mieru", "0.0.16")
	if err != nil {
		t.Fatalf("NewHandlers() error = %v", err)
	}
	first, err := h.newProtocolCredentialSecret(model.ProtocolEndpoint{Protocol: "mieru"})
	if err != nil {
		t.Fatalf("first newProtocolCredentialSecret() error = %v", err)
	}
	second, err := h.newProtocolCredentialSecret(model.ProtocolEndpoint{Protocol: "mieru"})
	if err != nil {
		t.Fatalf("second newProtocolCredentialSecret() error = %v", err)
	}
	if first == second || len(first) < 40 || len(second) < 40 {
		t.Fatalf("Mieru subscription credentials are not independent high-entropy values")
	}
	if h.zeroKernelContract() != "native-local-mieru" {
		t.Fatalf("zeroKernelContract() = %q", h.zeroKernelContract())
	}
}

func TestManagedMieruUsersCarryPrincipalAndIndependentCredential(t *testing.T) {
	h, err := NewHandlers(nil, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "", "native-local-mieru", "0.0.16")
	if err != nil {
		t.Fatalf("NewHandlers() error = %v", err)
	}
	encrypted, err := h.credentialCipher.Encrypt("subscription-secret")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	users, err := h.managedMieruUsers([]runtimeCredentialContext{{
		Credential: model.ProtocolCredential{
			ID: 9, Secret: encrypted, PrincipalKey: "subscription:7:endpoint:3",
		},
		Subscription: model.Subscription{ID: 7, UpdatedAt: time.Unix(10, 0).UTC()},
	}})
	if err != nil {
		t.Fatalf("managedMieruUsers() error = %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("managedMieruUsers() count = %d", len(users))
	}
	user, ok := users[0].(map[string]interface{})
	if !ok {
		t.Fatalf("managed Mieru user = %#v", users[0])
	}
	if user["username"] != "subscription-secret" || user["password"] != "subscription-secret" ||
		user["principal_key"] != "subscription:7:endpoint:3" {
		t.Fatalf("managed Mieru user = %#v", user)
	}
}

func TestMieruMigrationFallbackGetsBoundedTemporaryPrincipal(t *testing.T) {
	fallback, err := mieruMigrationFallbackUser(42, map[string]interface{}{
		"users": []interface{}{map[string]interface{}{"password": "endpoint-secret"}},
	})
	if err != nil {
		t.Fatalf("mieruMigrationFallbackUser() error = %v", err)
	}
	if fallback["username"] != "endpoint-secret" || fallback["password"] != "endpoint-secret" ||
		fallback["principal_key"] != "migration:endpoint:42" {
		t.Fatalf("Mieru migration fallback = %#v", fallback)
	}
	if _, err := mieruMigrationFallbackUser(42, map[string]interface{}{"users": []interface{}{}}); err == nil {
		t.Fatal("Mieru migration fallback accepted a missing endpoint credential")
	}
	if !isMieruMigrationPrincipal("migration:endpoint:42") ||
		isMieruMigrationPrincipal("migration:endpoint:0") ||
		isMieruMigrationPrincipal("subscription:42") {
		t.Fatal("Mieru migration principal classifier is not bounded")
	}
}

func TestMieruReadinessWaitsForFallbackCleanup(t *testing.T) {
	endpoint := model.ProtocolEndpoint{Protocol: "mieru"}
	if !includeMieruMigrationFallback(endpoint, false) {
		t.Fatal("first principal-aware publication must retain the migration fallback")
	}
	if includeMieruMigrationFallback(endpoint, true) {
		t.Fatal("cleanup publication must remove the migration fallback")
	}
	if includeMieruMigrationFallback(model.ProtocolEndpoint{Protocol: "mieru", MieruPrincipalReady: true}, false) {
		t.Fatal("ready endpoints must never reintroduce the migration fallback")
	}
	if mieruReadinessCanCommit(true, 1, false) {
		t.Fatal("readiness must not commit after only the compatibility publication")
	}
	if !mieruReadinessCanCommit(true, 1, true) {
		t.Fatal("readiness must commit after the fallback-free publication succeeds")
	}
	if !mieruReadinessCanCommit(true, 0, false) {
		t.Fatal("nodes without a migration fallback should commit in one publication")
	}
	if !mieruReadinessCanCommit(false, 1, false) {
		t.Fatal("legacy publication must be allowed to commit the reverse transition")
	}
}

func TestLegacyRuntimeUsersDoNotEmitPanelOnlyCredentialID(t *testing.T) {
	h, err := NewHandlers(nil, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "", "legacy", "")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := h.credentialCipher.Encrypt("11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	inbounds, err := h.legacyRuntimeInboundsForEndpoint(
		model.ProtocolEndpoint{ID: 2, Protocol: "vless", Port: 443},
		map[string]interface{}{"type": "vless", "users": []interface{}{}},
		[]model.ProtocolCredential{{
			ID: 7, CredentialID: "subscription-3-endpoint-2",
			PrincipalKey: "subscription:3:endpoint:2", Secret: secret,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(inbounds)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "credential_id") {
		t.Fatalf("runtime config leaked panel-only credential_id: %s", payload)
	}
	if !strings.Contains(string(payload), `"principal_key":"subscription:3:endpoint:2"`) {
		t.Fatalf("runtime config lost principal_key: %s", payload)
	}
}

func TestMieruContractRequiresRealZeroPreviewValidation(t *testing.T) {
	previous := managedZeroSubscriptionValidator
	t.Cleanup(func() { managedZeroSubscriptionValidator = previous })
	called := 0
	managedZeroSubscriptionValidator = func(_ context.Context, artifactDir, version string, config []byte) error {
		called++
		if artifactDir != "/artifacts" || version != "0.0.16" || !strings.Contains(string(config), `"type":"mieru"`) {
			t.Fatalf("validator inputs = %q %q %s", artifactDir, version, config)
		}
		return errors.New("future Zero rejected preview")
	}
	h := &handlers{zeroNativeAccess: true, zeroMieruAccess: true, zeroArtifactDir: "/artifacts", zeroLocalVersion: "0.0.16"}
	err := h.validateZeroSubscriptionPreview(context.Background(), subscriptionRendererZnetSink, `{"outbounds":[{"protocol":{"type":"mieru"}}]}`)
	if err == nil || !strings.Contains(err.Error(), "future Zero rejected preview") || called != 1 {
		t.Fatalf("validateZeroSubscriptionPreview() error = %v, calls = %d", err, called)
	}

	legacy := &handlers{}
	if err := legacy.validateZeroSubscriptionPreview(context.Background(), subscriptionRendererZnetSink, `{}`); err != nil || called != 1 {
		t.Fatalf("legacy preview unexpectedly invoked managed Zero: error=%v calls=%d", err, called)
	}
}
