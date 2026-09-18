package handler

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestPrepareMieruEndpointConfigsGeneratesAndHidesCredential(t *testing.T) {
	h, err := newTestHandlers(nil, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "", "legacy", "")
	if err != nil {
		t.Fatalf("newTestHandlers() error = %v", err)
	}

	serverRaw, clientRaw, err := prepareMieruEndpointConfigsWithExisting(
		`{"type":"mieru","users":[]}`,
		`{"type":"mieru","username":"admin-value","password":"admin-value","transport":"tcp"}`,
		"",
	)
	if err != nil {
		t.Fatalf("prepareMieruEndpointConfigsWithExisting() error = %v", err)
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

func TestReconcileMieruEndpointCredentialsPersistsTemplateAndQueuesPublication(t *testing.T) {
	h := openPublishTestHandlers(t, filepath.Join(t.TempDir(), "mieru-reconcile.db"))
	cipher := h.credentialCipher
	node := model.Node{ID: 41, Name: "mieru-node", Address: "192.0.2.41", Config: "{}", LifecycleStatus: "active"}
	if err := h.db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	serverCiphertext, err := cipher.Encrypt(`{"type":"mieru","users":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{
		ID: 42, NodeID: node.ID, Name: "Mieru", RuntimeKey: "00000000-0000-4000-8000-000000000042",
		Protocol: "mieru", Address: "mieru.example.test", Port: 2999, PublicPort: 2999,
		MultiplierMilli: 1000, ServerConfig: serverCiphertext,
		ClientConfig:   `{"type":"mieru","username":"legacy","password":"legacy","transport":"tcp"}`,
		OptionalConfig: "{}", Tags: "[]", IsActive: true, MieruPrincipalReady: true,
	}
	if err := h.db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.ReconcileMieruEndpointCredentials(); err != nil {
		t.Fatal(err)
	}
	if err := h.db.First(&endpoint, endpoint.ID).Error; err != nil {
		t.Fatal(err)
	}
	serverRaw, err := cipher.Decrypt(endpoint.ServerConfig)
	if err != nil || !strings.Contains(serverRaw, `"password"`) || strings.Contains(endpoint.ClientConfig, "legacy") {
		t.Fatalf("server=%s client=%s error=%v", serverRaw, endpoint.ClientConfig, err)
	}
	var publication model.NodeConfigPublish
	if err := h.db.First(&publication, "node_id = ?", node.ID).Error; err != nil || publication.EndpointID != endpoint.ID {
		t.Fatalf("publication=%+v error=%v", publication, err)
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
	h, err := newTestHandlers(nil, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "", "native-local-mieru", "0.0.16")
	if err != nil {
		t.Fatalf("newTestHandlers() error = %v", err)
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

func TestMieruReadinessWaitsForFallbackCleanup(t *testing.T) {
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
