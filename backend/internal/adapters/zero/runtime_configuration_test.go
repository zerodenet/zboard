package zero

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

type runtimeConfigurationCipher struct{}

func (runtimeConfigurationCipher) Encrypt(value string) (string, error) { return "enc:" + value, nil }
func (runtimeConfigurationCipher) Decrypt(value string) (string, error) {
	if !strings.HasPrefix(value, "enc:") {
		return "", errors.New("invalid ciphertext")
	}
	return strings.TrimPrefix(value, "enc:"), nil
}

func TestRuntimeConfigurationRendererBuildsManagedEndpointAndNetworkEntry(t *testing.T) {
	now := time.Date(2026, time.September, 16, 5, 0, 0, 0, time.UTC)
	notAfter := now.Add(24 * time.Hour)
	renderer := RuntimeConfigurationRenderer{Cipher: runtimeConfigurationCipher{}}
	payload, digest, err := renderer.Render(RuntimeConfigurationRenderRequest{
		NodeID: 7, APIKey: "connector-secret", ZeroVersion: "0.1.0", NativeConnector: true, Now: now,
		Snapshot: network.RuntimeConfigurationSnapshot{
			SiteURL: "https://panel.example.test/",
			Endpoints: []network.RuntimeConfigurationEndpoint{{
				ID: 11, NodeID: 7, Protocol: "vless", Address: "node.example.test", Port: 443, PublicPort: 8443,
				ServerConfig:            `enc:{"type":"vless","tls":{},"users":[{"flow":"xtls-rprx-vision"}]}`,
				EgressConfig:            `enc:{"type":"socks5","server":"egress.example.test","port":1080,"username":"user","password":"pass"}`,
				ActiveSubscriptionCount: 1,
				Certificate:             &network.RuntimeConfigurationCertificate{Status: "active", CertPath: "/cert.pem", KeyPath: "/key.pem", NotAfter: &notAfter},
				Credentials: []network.RuntimeConfigurationCredential{{
					ID: 21, SubscriptionID: 31, PrincipalKey: "subscription:31", Secret: "enc:user-id", ListenPort: 443, PublicPort: 8443,
					SubscriptionUpdatedAt: now.Add(-time.Minute), SpeedLimitMbps: 8, DeviceLimit: 2, SoleActiveCredential: true,
				}},
			}},
			NetworkEntries: []network.RuntimeConfigurationNetworkEntry{{
				ID: 41, NodeID: 7, EndpointID: 51, Network: "tcp_udp", Port: 9443,
				Landing: network.RuntimeConfigurationLanding{EndpointExists: true, NodeExists: true, Protocol: "vmess", Address: "landing.example.test", Port: 1443, PublicPort: 2443, Active: true, NodeEnabled: true, NodeLifecycleStatus: "active"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := sha256.Sum256(payload)
	if digest != hex.EncodeToString(wantDigest[:]) || payload[len(payload)-1] != '\n' {
		t.Fatalf("digest/newline mismatch: digest=%s payload=%q", digest, payload)
	}
	var config map[string]interface{}
	if err := json.Unmarshal(payload, &config); err != nil {
		t.Fatal(err)
	}
	inbounds, _ := config["inbounds"].([]interface{})
	if len(inbounds) != 2 {
		t.Fatalf("inbounds = %#v", config["inbounds"])
	}
	managed := inbounds[0].(map[string]interface{})["protocol"].(map[string]interface{})
	users := managed["users"].([]interface{})
	user := users[0].(map[string]interface{})
	if user["id"] != "user-id" || user["principal_key"] != "subscription:31" || user["up_bps"] != float64(1_000_000) || user["device_limit"] != float64(2) {
		t.Fatalf("managed user = %#v", user)
	}
	tls := managed["tls"].(map[string]interface{})
	if tls["cert_path"] != "/cert.pem" || tls["key_path"] != "/key.pem" {
		t.Fatalf("managed certificate = %#v", tls)
	}
	api := config["api"].(map[string]interface{})
	if _, exists := config["push"]; exists || api["outbox_path"] != "/var/lib/zerodenet/event-outbox.jsonl" {
		t.Fatalf("connector configuration = %#v", config)
	}
	outbounds := config["outbounds"].([]interface{})
	if len(outbounds) != 1 || outbounds[0].(map[string]interface{})["tag"] != "endpoint-11-egress" {
		t.Fatalf("endpoint egress outbounds = %#v", outbounds)
	}
	rules := config["route"].(map[string]interface{})["rules"].([]interface{})
	egressRule := rules[0].(map[string]interface{})
	if egressRule["action"].(map[string]interface{})["outbound"] != "endpoint-11-egress" {
		t.Fatalf("endpoint egress route = %#v", rules)
	}
}

func TestRuntimeConfigurationRendererUsesBootstrapOnlyWithoutRealInbound(t *testing.T) {
	payload, _, err := (RuntimeConfigurationRenderer{Cipher: runtimeConfigurationCipher{}}).Render(RuntimeConfigurationRenderRequest{
		NodeID: 1, APIKey: "connector", ZeroVersion: "0.1.0", NativeConnector: true, Now: time.Now().UTC(),
		Snapshot: network.RuntimeConfigurationSnapshot{SiteURL: "https://panel.example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Inbounds []struct {
			Tag      string                 `json:"tag"`
			Listen   map[string]interface{} `json:"listen"`
			Protocol map[string]interface{} `json:"protocol"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(payload, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Inbounds) != 1 || config.Inbounds[0].Tag != "zboard-control-bootstrap" || config.Inbounds[0].Listen["address"] != "127.0.0.1" || config.Inbounds[0].Listen["port"] != float64(0) || config.Inbounds[0].Protocol["type"] != "direct" {
		t.Fatalf("bootstrap = %#v", config.Inbounds)
	}
}

func TestRuntimeConfigurationRendererRejectsMissingCredentialProjection(t *testing.T) {
	_, _, err := (RuntimeConfigurationRenderer{Cipher: runtimeConfigurationCipher{}}).Render(RuntimeConfigurationRenderRequest{
		NodeID: 1, APIKey: "connector", ZeroVersion: "0.1.0", NativeConnector: true, Now: time.Now().UTC(),
		Snapshot: network.RuntimeConfigurationSnapshot{SiteURL: "https://panel.example.test", Endpoints: []network.RuntimeConfigurationEndpoint{{
			ID: 2, NodeID: 1, Protocol: "vless", Port: 443, ServerConfig: `enc:{"type":"vless","users":[]}`, ActiveSubscriptionCount: 2,
		}}},
	})
	if err == nil || !strings.Contains(err.Error(), "active subscriptions but no active credentials") {
		t.Fatalf("error = %v", err)
	}
}

func TestRuntimeConfigurationRendererOwnsManagedAudienceSemantics(t *testing.T) {
	renderer := RuntimeConfigurationRenderer{Cipher: runtimeConfigurationCipher{}}
	for _, protocolName := range []string{"vless", "vmess", "trojan"} {
		t.Run(protocolName+"_empty_audience", func(t *testing.T) {
			inbounds, err := renderer.renderEndpoint(network.RuntimeConfigurationEndpoint{ID: 7, Protocol: protocolName, Port: 443}, map[string]interface{}{"type": protocolName, "users": []interface{}{}}, false)
			if err != nil || len(inbounds) != 1 {
				t.Fatalf("inbounds=%d error=%v", len(inbounds), err)
			}
		})
	}
	for _, protocolName := range []string{"hysteria2", "mieru"} {
		t.Run(protocolName+"_waits_for_audience", func(t *testing.T) {
			inbounds, err := renderer.renderEndpoint(network.RuntimeConfigurationEndpoint{ID: 8, Protocol: protocolName, Port: 443, MieruPrincipalReady: true}, map[string]interface{}{"type": protocolName, "users": []interface{}{}}, false)
			if err != nil || len(inbounds) != 0 {
				t.Fatalf("inbounds=%d error=%v", len(inbounds), err)
			}
		})
	}
	credential := network.RuntimeConfigurationCredential{ID: 9, PrincipalKey: "subscription:7:endpoint:8", Secret: "enc:subscriber-secret"}
	for _, protocolName := range []string{"hysteria2", "mieru"} {
		t.Run(protocolName+"_materialized_audience", func(t *testing.T) {
			inbounds, err := renderer.renderEndpoint(network.RuntimeConfigurationEndpoint{ID: 8, Protocol: protocolName, Port: 443, ActiveSubscriptionCount: 1, Credentials: []network.RuntimeConfigurationCredential{credential}, MieruPrincipalReady: true}, map[string]interface{}{"type": protocolName, "users": []interface{}{}}, false)
			if err != nil || len(inbounds) != 1 {
				t.Fatalf("inbounds=%d error=%v", len(inbounds), err)
			}
			user := inbounds[0]["protocol"].(map[string]interface{})["users"].([]interface{})[0].(map[string]interface{})
			if user["password"] != "subscriber-secret" {
				t.Fatalf("user=%#v", user)
			}
		})
	}
}

func TestRuntimeConfigurationRendererProjectsOnlySafePerProcessPolicy(t *testing.T) {
	now := time.UnixMilli(1_753_500_000_123).UTC()
	sole := network.RuntimeConfigurationCredential{
		PrincipalKey: "subscription:7:endpoint:3", SubscriptionID: 7,
		SubscriptionUpdatedAt: now, SpeedLimitMbps: 80, DeviceLimit: 3, SoleActiveCredential: true,
	}
	user, err := managedAccessUserFields(sole)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]interface{}{
		"principal_key": "subscription:7:endpoint:3", "policy_revision": uint64(now.UnixMilli()),
		"up_bps": uint64(10_000_000), "down_bps": uint64(10_000_000), "device_limit": uint32(3),
	} {
		if user[key] != want {
			t.Errorf("%s=%#v want=%#v", key, user[key], want)
		}
	}
	sole.SoleActiveCredential = false
	distributed, err := managedAccessUserFields(sole)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"up_bps", "down_bps", "device_limit"} {
		if _, exists := distributed[field]; exists {
			t.Fatalf("unsafe distributed field %s projected: %#v", field, distributed)
		}
	}
}

func TestRuntimeConfigurationRendererOwnsShadowsocks2022Rules(t *testing.T) {
	renderer := RuntimeConfigurationRenderer{Cipher: runtimeConfigurationCipher{}}
	protocol := map[string]interface{}{
		"type": "shadowsocks", "cipher": "2022-blake3-aes-256-gcm", "password": "identity-secret",
	}
	inbounds, err := renderer.renderEndpoint(network.RuntimeConfigurationEndpoint{
		ID: 4, Protocol: "shadowsocks", Port: 8388, ActiveSubscriptionCount: 1,
		Credentials: []network.RuntimeConfigurationCredential{{ID: 1, PrincipalKey: "subscription:1:endpoint:4", Secret: "enc:user-secret"}},
	}, protocol, false)
	if err != nil || len(inbounds) != 1 {
		t.Fatalf("inbounds=%d error=%v", len(inbounds), err)
	}
	if protocol["identity_password"] != "identity-secret" {
		t.Fatalf("protocol=%#v", protocol)
	}
	_, err = renderer.renderEndpoint(network.RuntimeConfigurationEndpoint{
		ID: 4, Protocol: "shadowsocks", Port: 8388, ActiveSubscriptionCount: 2,
		Credentials: []network.RuntimeConfigurationCredential{{ID: 1, Secret: "enc:first"}, {ID: 2, Secret: "enc:second"}},
	}, map[string]interface{}{"type": "shadowsocks", "cipher": "2022-blake3-chacha20-poly1305"}, false)
	if err == nil || !strings.Contains(err.Error(), "cannot identify multiple managed users") {
		t.Fatalf("error=%v", err)
	}
}

func TestRuntimeConfigurationRendererOwnsMieruMigrationFallback(t *testing.T) {
	inbounds, err := (RuntimeConfigurationRenderer{Cipher: runtimeConfigurationCipher{}}).renderEndpoint(
		network.RuntimeConfigurationEndpoint{ID: 42, Protocol: "mieru", Port: 443},
		map[string]interface{}{"type": "mieru", "users": []interface{}{map[string]interface{}{"password": "endpoint-secret"}}}, false,
	)
	if err != nil || len(inbounds) != 1 {
		t.Fatalf("inbounds=%d error=%v", len(inbounds), err)
	}
	user := inbounds[0]["protocol"].(map[string]interface{})["users"].([]interface{})[0].(map[string]interface{})
	if user["username"] != "endpoint-secret" || user["password"] != "endpoint-secret" || user["principal_key"] != "migration:endpoint:42" {
		t.Fatalf("fallback=%#v", user)
	}
}
