package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func networkEntryFixture(t *testing.T) (trafficReadFixture, model.Node, model.ProtocolEndpoint) {
	t.Helper()
	f := newTrafficReadFixture(t)
	now := time.Now().UTC()
	a := model.Node{Name: "A", Address: "entry.example.test", IsEnabled: true, LastSeenAt: &now, Config: "{}"}
	b := model.Node{Name: "B", Address: "landing.example.test", IsEnabled: true, LastSeenAt: &now, Config: "{}"}
	for _, row := range []interface{}{&a, &b, &model.Installation{ID: 1, SiteURL: "https://panel.example.test"}} {
		if err := f.h.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	endpoint := model.ProtocolEndpoint{NodeID: b.ID, Name: "B TLS", RuntimeKey: "landing", Protocol: "trojan", Address: b.Address, Port: 443, PublicPort: 1443, IsActive: true, ClientConfig: `{"type":"trojan","server":"landing.example.test","port":1443,"password":"b-user-secret"}`, OptionalConfig: "{}", Tags: "[]"}
	if err := f.h.db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	return f, a, endpoint
}
func saveEntryForTest(t *testing.T, f trafficReadFixture, a model.Node, b model.ProtocolEndpoint, path string) model.NetworkEntry {
	t.Helper()
	payload := fmt.Sprintf(`{"name":"A entry","node_id":%d,"endpoint_id":%d,"address":"entry.example.test","port":12345,"public_port":23456,"enabled":true%s}`, a.ID, b.ID, path)
	w := httptest.NewRecorder()
	f.h.NetworkEntriesHandler(w, announcementRequest(http.MethodPost, "/api/v1/admin/network-entries", f.admin, payload))
	if w.Code != 200 {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	var response struct{ Data model.NetworkEntry }
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}
func TestNetworkEntryForwardingAndSubscriptionKeepLandingIdentity(t *testing.T) {
	f, a, b := networkEntryFixture(t)
	entry := saveEntryForTest(t, f, a, b, "")
	payload, _, err := f.h.compileNodeRuntimeConfig(a, "connector-fixture", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal(payload, &config); err != nil {
		t.Fatal(err)
	}
	inbounds := config["inbounds"].([]interface{})
	if len(inbounds) != 1 {
		t.Fatalf("inbounds=%v", inbounds)
	}
	inbound := inbounds[0].(map[string]interface{})
	protocol := inbound["protocol"].(map[string]interface{})
	if inbound["tag"] != networkEntryTag(entry.ID) || protocol["type"] != "direct" || protocol["target"] != b.Address || protocol["port"] != float64(1443) {
		t.Fatalf("forwarding=%v", inbound)
	}
	if strings.Contains(string(payload), "b-user-secret") || strings.Contains(string(payload), `"users"`) {
		t.Fatal("B user credentials leaked to A")
	}
	base := subscriptionManifestNode{ID: b.ID, NodeID: b.NodeID, SubscriptionID: 42, CredentialID: "B-credential", Name: b.Name, Address: b.Address, Port: 443, PublicPort: 1443, Protocol: b.Protocol, MultiplierMilli: 2000, Config: json.RawMessage(b.ClientConfig)}
	nodes, err := f.h.projectNetworkEntries([]subscriptionManifestNode{base}, time.Now())
	if err != nil || len(nodes) != 1 {
		t.Fatalf("unpublished entry leaked: %v %v", nodes, err)
	}
	if err := f.h.db.Where("node_id = ?", a.ID).Delete(&model.NodeConfigPublish{}).Error; err != nil {
		t.Fatal(err)
	}
	nodes, err = f.h.projectNetworkEntries([]subscriptionManifestNode{base}, time.Now())
	if err != nil || len(nodes) != 2 {
		t.Fatalf("nodes=%v err=%v", nodes, err)
	}
	if string(nodes[0].Config) != b.ClientConfig {
		t.Fatal("direct config modified")
	}
	front := nodes[1]
	var client map[string]interface{}
	_ = json.Unmarshal(front.Config, &client)
	if front.NodeID != b.NodeID || front.ID != b.ID || front.SubscriptionID != 42 || front.CredentialID != "B-credential" || front.MultiplierMilli != 2000 || front.Address != entry.Address || front.PublicPort != 23456 || client["password"] != "b-user-secret" || client["sni"] != b.Address {
		t.Fatalf("front lost B semantics: %+v %v", front, client)
	}
	if nodes, err := f.h.projectNetworkEntries(nil, time.Now()); err != nil || len(nodes) != 0 {
		t.Fatal("front entry bypassed B access filtering")
	}
	if err := f.h.db.Model(&entry).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	nodes, err = f.h.projectNetworkEntries([]subscriptionManifestNode{base}, time.Now())
	if err != nil || len(nodes) != 1 {
		t.Fatal("disabled entry delivered")
	}
}

func TestNetworkEntryPathIsolationEncryptionAndRealZeroValidation(t *testing.T) {
	f, a, b := networkEntryFixture(t)
	old := managedZeroSubscriptionValidator
	t.Cleanup(func() { managedZeroSubscriptionValidator = old })
	managedZeroSubscriptionValidator = func(ctx context.Context, dir, version string, payload []byte) error {
		if binary := os.Getenv("ZBOARD_ZERO_VALIDATE_BIN"); binary != "" {
			path := filepath.Join(t.TempDir(), "entry.json")
			if err := os.WriteFile(path, payload, 0600); err != nil {
				return err
			}
			out, err := exec.CommandContext(ctx, binary, "validate", path).CombinedOutput()
			if err != nil {
				return fmt.Errorf("validate: %w %s", err, out)
			}
		}
		return nil
	}
	path := `,"path_config":{"outbounds":[{"tag":"first","protocol":{"type":"socks5","server":"192.0.2.1","port":1080,"password":"path-secret","username":"test"}},{"tag":"last","protocol":{"type":"shadowsocks","cipher":"aes-128-gcm","password":"path-secret","server":"192.0.2.2","port":1080}}],"outbound_groups":[{"tag":"chain","type":"relay","proxies":["first","last"]},{"tag":"auto","type":"url_test","outbounds":["first","last"]},{"tag":"select","type":"selector","outbounds":["chain","auto"],"default":"chain","selected":"auto"}],"target":"select"}`
	entry := saveEntryForTest(t, f, a, b, path)
	var stored model.NetworkEntry
	if err := f.h.db.First(&stored, entry.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.PathConfig == "" || strings.Contains(stored.PathConfig, "path-secret") {
		t.Fatal("proxy credential not encrypted")
	}
	var list []networkEntryView
	f.get(t, "/api/v1/admin/network-entries", true, f.h.NetworkEntriesHandler, &list)
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), "path-secret") || strings.Contains(string(raw), stored.PathConfig) {
		t.Fatal("path credentials returned from list")
	}
	payload, _, err := f.h.compileNodeRuntimeConfig(a, "fixture", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	_ = json.Unmarshal(payload, &config)
	groups := config["outbound_groups"].([]interface{})
	if groups[0].(map[string]interface{})["proxies"].([]interface{})[0] != fmt.Sprintf("entry-%d/first", entry.ID) {
		t.Fatalf("chain not scoped: %s", payload)
	}
	if err := managedZeroSubscriptionValidator(context.Background(), "", "", payload); err != nil {
		t.Fatal(err)
	}
	base := subscriptionManifestNode{ID: b.ID, NodeID: b.NodeID, Name: b.Name, Address: b.Address, Protocol: b.Protocol, Config: json.RawMessage(b.ClientConfig)}
	_ = f.h.db.Where("node_id = ?", a.ID).Delete(&model.NodeConfigPublish{}).Error
	nodes, err := f.h.projectNetworkEntries([]subscriptionManifestNode{base}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(nodes)
	if strings.Contains(string(raw), "path-secret") {
		t.Fatal("path credentials leaked to subscriber")
	}
}

func TestNetworkEntryCRUDConflictsAndPublicationDependencies(t *testing.T) {
	f, a, b := networkEntryFixture(t)
	entry := saveEntryForTest(t, f, a, b, "")
	for _, test := range []struct {
		name   string
		method string
		path   string
		body   string
		token  string
		status int
	}{
		{"admin only", http.MethodGet, "/api/v1/admin/network-entries", "", f.token, 403},
		{"same port", http.MethodPost, "/api/v1/admin/network-entries", fmt.Sprintf(`{"name":"duplicate","node_id":%d,"endpoint_id":%d,"address":"entry.test","port":12345,"enabled":true}`, a.ID, b.ID), f.admin, 400},
		{"loop", http.MethodPost, "/api/v1/admin/network-entries", fmt.Sprintf(`{"name":"loop","node_id":%d,"endpoint_id":%d,"address":"entry.test","port":5555,"enabled":true}`, b.NodeID, b.ID), f.admin, 400},
		{"stale edit", http.MethodPut, fmt.Sprintf("/api/v1/admin/network-entries/%d", entry.ID), `{"revision":0,"name":"stale","address":"entry.test","port":5555}`, f.admin, 409},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			f.h.NetworkEntriesHandler(w, announcementRequest(test.method, test.path, test.token, test.body))
			if w.Code != test.status {
				t.Fatalf("status=%d %s", w.Code, w.Body.String())
			}
		})
	}
	if err := f.h.db.Where("1=1").Delete(&model.NodeConfigPublish{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := enqueueNodeConfigPublish(f.h.db, b.NodeID, b.ID, 99); err != nil {
		t.Fatal(err)
	}
	var queued []model.NodeConfigPublish
	if err := f.h.db.Order("node_id").Find(&queued).Error; err != nil || len(queued) != 2 {
		t.Fatalf("dependent publication=%v %v", queued, err)
	}
	w := httptest.NewRecorder()
	f.h.NodeCascadeDeleteHandler(w, announcementRequest(http.MethodDelete, fmt.Sprintf("/api/v1/nodes/%d", a.ID), f.admin, ""))
	if w.Code != 409 {
		t.Fatalf("node deletion not guarded: %d", w.Code)
	}
	w = httptest.NewRecorder()
	f.h.NetworkEntriesHandler(w, announcementRequest(http.MethodDelete, fmt.Sprintf("/api/v1/admin/network-entries/%d", entry.ID), f.admin, ""))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	payload, _, err := f.h.compileNodeRuntimeConfig(a, "fixture", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), fmt.Sprintf(`"tag": "entry-%d"`, entry.ID)) {
		t.Fatal("deleted entry remains in runtime")
	}
	// A-only publication must permit a nullable protocol foreign key.
	deployment := model.ProtocolDeployment{NodeID: a.ID, Status: "running", ConfigRevision: 1}
	if err := f.h.db.Create(&deployment).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	f.h.db.Model(&model.ProtocolDeployment{}).Where("id = ? AND protocol_endpoint_id IS NULL", deployment.ID).Count(&count)
	if count != 1 {
		t.Fatal("entry-only publication used endpoint id zero instead of NULL")
	}
}

func TestNetworkEntryTransportCompatibility(t *testing.T) {
	raw := `{"outbounds":[{"tag":"one","protocol":{"type":"socks5"}},{"tag":"two","protocol":{"type":"socks5"}}],"outbound_groups":[{"tag":"chain","type":"relay","proxies":["one","two"]}],"target":"chain"}`
	var path networkEntryPath
	if err := json.Unmarshal([]byte(raw), &path); err != nil {
		t.Fatal(err)
	}
	if err := path.validateDatagramPath(); err == nil || !strings.Contains(err.Error(), "最后一跳") {
		t.Fatalf("unsupported UDP final hop accepted: %v", err)
	}
	config := map[string]interface{}{"route": map[string]interface{}{"rules": []interface{}{}}}
	if err := path.appendTo(config, model.NetworkEntry{ID: 9, Network: "tcp"}); err != nil {
		t.Fatalf("valid TCP-only chain rejected: %v", err)
	}
	f, a, b := networkEntryFixture(t)
	if err := f.h.db.Model(&b).Update("protocol", "hysteria2").Error; err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"name":"bad HY2","network":"tcp","node_id":%d,"endpoint_id":%d,"address":"entry.test","port":3333,"enabled":true}`, a.ID, b.ID)
	w := httptest.NewRecorder()
	f.h.NetworkEntriesHandler(w, announcementRequest(http.MethodPost, "/api/v1/admin/network-entries", f.admin, payload))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "Hysteria2") {
		t.Fatalf("invalid transport accepted %d %s", w.Code, w.Body.String())
	}
}

func TestNetworkEntryPreservesExplicitTLSAndDisablesOnlyRawShadowsocksUDP(t *testing.T) {
	config := map[string]interface{}{"tls": map[string]interface{}{"server_name": "cert.example.test"}}
	preserveNetworkEntryPeerIdentity(config, "landing.example.test", "trojan", 443)
	if config["sni"] != "cert.example.test" {
		t.Fatal("explicit B TLS identity overwritten")
	}
	endpoint := subscriptionTemplateEndpoint{ID: 1, NetworkEntryID: 2, NetworkEntryNetwork: "tcp", Name: "front", Address: "entry.test", PublicPort: 1234, Protocol: "shadowsocks", Config: map[string]interface{}{"password": "fixture", "cipher": "aes-128-gcm"}}
	clash, err := clashProxy(endpoint)
	if err != nil || clash["udp"] != false {
		t.Fatalf("Clash UDP=%v err=%v", clash, err)
	}
	singbox, err := singBoxOutbound(endpoint)
	if err != nil || singbox["network"] != "tcp" {
		t.Fatalf("sing-box network=%v err=%v", singbox, err)
	}
	endpoint.NetworkEntryID = 0
	endpoint.NetworkEntryNetwork = ""
	clash, err = clashProxy(endpoint)
	if err != nil || clash["udp"] != true {
		t.Fatal("B direct UDP was disabled")
	}
}

func TestNetworkEntryRejectsRetainedTCPPathWhenSwitchingToUDP(t *testing.T) {
	f, a, b := networkEntryFixture(t)
	encrypted, err := f.h.credentialCipher.Encrypt(`{"outbounds":[{"tag":"a","protocol":{"type":"socks5"}},{"tag":"b","protocol":{"type":"socks5"}}],"outbound_groups":[{"tag":"chain","type":"relay","proxies":["a","b"]}],"target":"chain"}`)
	if err != nil {
		t.Fatal(err)
	}
	entry := model.NetworkEntry{Name: "existing TCP", Network: "tcp", NodeID: a.ID, EndpointID: b.ID, Address: a.Address, Port: 12345, PublicPort: 12345, PathConfig: encrypted, Enabled: true, Revision: 1}
	if err := f.h.db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"name":"existing TCP","network":"tcp_udp","node_id":%d,"endpoint_id":%d,"address":"entry.test","port":12345,"enabled":true,"revision":1}`, a.ID, b.ID)
	w := httptest.NewRecorder()
	f.h.NetworkEntriesHandler(w, announcementRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/network-entries/%d", entry.ID), f.admin, payload))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "最后一跳") {
		t.Fatalf("retained incompatible path accepted: %d %s", w.Code, w.Body.String())
	}
	if err := f.h.db.First(&entry, entry.ID).Error; err != nil || entry.Network != "tcp" || entry.Revision != 1 {
		t.Fatal("rejected edit overwrote working entry")
	}
}

func TestNetworkEntryPreventsMovingLandingProtocolOntoEntryNode(t *testing.T) {
	f, a, b := networkEntryFixture(t)
	server, err := f.h.credentialCipher.Encrypt(`{"type":"vless","users":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&b).Updates(map[string]interface{}{"protocol": "vless", "server_config": server, "client_config": `{"type":"vless","server":"landing.example.test","port":1443}`}).Error; err != nil {
		t.Fatal(err)
	}
	saveEntryForTest(t, f, a, b, "")
	payload := fmt.Sprintf(`{"node_id":%d,"name":"moved landing","protocol":"vless","address":"entry.test","port":2443,"public_port":2443,"multiplier_milli":1000,"is_active":true,"config":"{\"type\":\"vless\",\"users\":[]}","client_config":"{\"type\":\"vless\",\"server\":\"entry.test\",\"port\":2443}","optional_config":"{}","tags":"[]"}`, a.ID)
	w := httptest.NewRecorder()
	f.h.ProtocolEndpointUpdateHandler(w, announcementRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/protocol-endpoints/%d", b.ID), f.admin, payload))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "网络前置冲突") {
		t.Fatalf("landing move accepted: %d %s", w.Code, w.Body.String())
	}
	if err := f.h.db.First(&b, b.ID).Error; err != nil || b.NodeID == a.ID {
		t.Fatal("rejected landing move changed binding")
	}
}

func TestNetworkEntryPreservesLandingHTTPAuthority(t *testing.T) {
	config := map[string]interface{}{"ws": map[string]interface{}{}, "h2": map[string]interface{}{}}
	preserveNetworkEntryPeerIdentity(config, "landing.example.test", "vless", 2443)
	headers := configMap(configMap(config, "ws"), "headers")
	if headers["Host"] != "landing.example.test:2443" || configMap(config, "h2")["host"] != "landing.example.test:2443" {
		t.Fatalf("missing landing authority: %v", config)
	}
	config = map[string]interface{}{"ws": map[string]interface{}{"headers": map[string]interface{}{"hOsT": "cdn.example.test"}}}
	preserveNetworkEntryPeerIdentity(config, "landing.example.test", "vless", 2443)
	headers = configMap(configMap(config, "ws"), "headers")
	if len(headers) != 1 || headers["hOsT"] != "cdn.example.test" {
		t.Fatalf("explicit Host changed: %v", headers)
	}
}
