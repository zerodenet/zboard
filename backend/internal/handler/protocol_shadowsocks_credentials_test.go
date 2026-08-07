package handler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestManagedShadowsocksRuntimeProtocolSharesConfiguredPortUsers(t *testing.T) {
	h, err := NewHandlers(nil, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "", "native-local-mieru", "0.0.16")
	if err != nil {
		t.Fatal(err)
	}
	first, err := h.credentialCipher.Encrypt("first-secret")
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.credentialCipher.Encrypt("second-secret")
	if err != nil {
		t.Fatal(err)
	}
	protocol := map[string]interface{}{
		"type":     "shadowsocks",
		"cipher":   "chacha20-ietf-poly1305",
		"password": "unused-endpoint-secret",
	}
	contexts := []runtimeCredentialContext{
		{
			Credential:   model.ProtocolCredential{ID: 3, Secret: first, PrincipalKey: "subscription:1:endpoint:4"},
			Subscription: model.Subscription{ID: 1, UpdatedAt: time.Unix(10, 0).UTC()},
		},
		{
			Credential:   model.ProtocolCredential{ID: 13, Secret: second, PrincipalKey: "subscription:2:endpoint:4"},
			Subscription: model.Subscription{ID: 2, UpdatedAt: time.Unix(11, 0).UTC()},
		},
	}
	if err := h.managedShadowsocksRuntimeProtocol(protocol, contexts); err != nil {
		t.Fatalf("managedShadowsocksRuntimeProtocol() error = %v", err)
	}
	users, ok := protocol["users"].([]interface{})
	if !ok || len(users) != 2 {
		t.Fatalf("users = %#v, want two managed users", protocol["users"])
	}
	if _, exists := protocol["password"]; exists {
		t.Fatal("managed Shadowsocks runtime retained the endpoint password")
	}
	if _, exists := protocol["identity_password"]; exists {
		t.Fatal("legacy AEAD Shadowsocks runtime emitted SIP023 identity_password")
	}
}

func TestManagedShadowsocksRuntimeProtocolUsesAES2022Identity(t *testing.T) {
	h, err := NewHandlers(nil, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "", "native-local-mieru", "0.0.16")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := h.credentialCipher.Encrypt("dXNlci1rZXktMzItYnl0ZXMtbG9uZy0wMDA=")
	if err != nil {
		t.Fatal(err)
	}
	protocol := map[string]interface{}{
		"type":     "shadowsocks",
		"cipher":   "2022-blake3-aes-256-gcm",
		"password": "aWRlbnRpdHkta2V5LTMyLWJ5dGVzLWxvbmc=",
	}
	if err := h.managedShadowsocksRuntimeProtocol(protocol, []runtimeCredentialContext{{
		Credential:   model.ProtocolCredential{ID: 3, Secret: secret, PrincipalKey: "subscription:1:endpoint:4"},
		Subscription: model.Subscription{ID: 1, UpdatedAt: time.Unix(10, 0).UTC()},
	}}); err != nil {
		t.Fatalf("managedShadowsocksRuntimeProtocol() error = %v", err)
	}
	if protocol["identity_password"] != "aWRlbnRpdHkta2V5LTMyLWJ5dGVzLWxvbmc=" {
		t.Fatalf("identity_password = %#v", protocol["identity_password"])
	}
}

func TestManagedShadowsocksRuntimeProtocolRejects2022ChachaMultiUser(t *testing.T) {
	h, err := NewHandlers(nil, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "", "native-local-mieru", "0.0.16")
	if err != nil {
		t.Fatal(err)
	}
	first, _ := h.credentialCipher.Encrypt("first")
	second, _ := h.credentialCipher.Encrypt("second")
	err = h.managedShadowsocksRuntimeProtocol(map[string]interface{}{
		"type": "shadowsocks", "cipher": shadowsocks2022ChachaCipher,
	}, []runtimeCredentialContext{
		{Credential: model.ProtocolCredential{ID: 1, Secret: first}},
		{Credential: model.ProtocolCredential{ID: 2, Secret: second}},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot identify multiple managed users") {
		t.Fatalf("error = %v", err)
	}
}

func TestProtocolCredentialClientPortUsesEndpointForShadowsocks(t *testing.T) {
	endpoint := model.ProtocolEndpoint{Protocol: "shadowsocks", PublicPort: 12855}
	credential := model.ProtocolCredential{PublicPort: 12857}
	if got := protocolCredentialClientPort(endpoint, credential); got != 12855 {
		t.Fatalf("port = %d, want 12855", got)
	}
	endpoint.Protocol = "trojan"
	if got := protocolCredentialClientPort(endpoint, credential); got != 12857 {
		t.Fatalf("non-Shadowsocks port = %d, want 12857", got)
	}
}

func TestManagedShadowsocksRuntimeProtocolProducesSerializableUsers(t *testing.T) {
	protocol := map[string]interface{}{
		"type": "shadowsocks", "cipher": "aes-256-gcm",
	}
	if err := configureManagedShadowsocksUsers(protocol, "aes-256-gcm", []interface{}{
		map[string]interface{}{"password": "secret", "principal_key": "subscription:1:endpoint:4"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(protocol); err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
}
