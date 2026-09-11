package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func stubProxyPoolSubscriptionFetch(t *testing.T, content string, fetchErr error) {
	t.Helper()
	previous := proxyPoolSubscriptionFetcher
	proxyPoolSubscriptionFetcher = func(context.Context, string, string) ([]byte, error) {
		return []byte(content), fetchErr
	}
	t.Cleanup(func() { proxyPoolSubscriptionFetcher = previous })
}

func poolValidationForTest(t *testing.T) {
	t.Helper()
	old := managedZeroSubscriptionValidator
	t.Cleanup(func() { managedZeroSubscriptionValidator = old })
	managedZeroSubscriptionValidator = func(ctx context.Context, dir, version string, payload []byte) error {
		if binary := os.Getenv("ZBOARD_ZERO_VALIDATE_BIN"); binary != "" {
			path := filepath.Join(t.TempDir(), "pool.json")
			if err := os.WriteFile(path, payload, 0600); err != nil {
				return err
			}
			out, err := exec.CommandContext(ctx, binary, "validate", path).CombinedOutput()
			if err != nil {
				t.Logf("Zero validation failed: %s", out)
				return fmt.Errorf("validate: %w %s", err, out)
			}
		}
		return nil
	}
}
func poolRequest(t *testing.T, f trafficReadFixture, method, path string, payload interface{}, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler(w, announcementRequest(method, path, f.admin, string(raw)))
	return w
}
func createPoolForTest(t *testing.T, f trafficReadFixture, nodeID uint) model.NodeProxyPool {
	t.Helper()
	var config interface{}
	json.Unmarshal([]byte(`{"outbounds":[{"tag":"p1","protocol":{"type":"socks5","server":"192.0.2.1","port":1080,"username":"internal","password":"pool-only-secret"}},{"tag":"p2","protocol":{"type":"shadowsocks","server":"192.0.2.2","port":443,"cipher":"aes-128-gcm","password":"pool-only-secret"}}],"outbound_groups":[{"tag":"auto","type":"url_test","outbounds":["p1","p2"],"interval_seconds":300}],"target":"auto"}`), &config)
	w := poolRequest(t, f, "POST", "/api/v1/admin/node-proxy-pools", map[string]interface{}{"node_id": nodeID, "name": "shared", "config": config}, f.h.NodeProxyPoolsHandler)
	if w.Code != 200 {
		t.Fatalf("pool create %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "pool-only-secret") {
		t.Fatal("pool leaked secret")
	}
	var result struct{ Data model.NodeProxyPool }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Data
}
func TestSharedPoolCompilesOnceAndDirectEntryBypassesIt(t *testing.T) {
	poolValidationForTest(t)
	f, a, b := networkEntryFixture(t)
	pool := createPoolForTest(t, f, a.ID)
	b2 := b
	b2.ID = 0
	b2.RuntimeKey = "landing-two"
	b2.Name = "other protocol port"
	b2.PublicPort = 2443
	if err := f.h.db.Create(&b2).Error; err != nil {
		t.Fatal(err)
	}
	for i, endpoint := range []model.ProtocolEndpoint{b, b2, b} {
		request := map[string]interface{}{"name": fmt.Sprintf("front-%d", i), "node_id": a.ID, "parent_protocol_id": endpoint.ID, "address": a.Address, "port": 12000 + i, "enabled": true, "network": "tcp_udp"}
		if i < 2 {
			request["proxy_pool_id"] = pool.ID
		}
		w := poolRequest(t, f, "POST", "/api/v1/admin/network-entries", request, f.h.NetworkEntriesHandler)
		if w.Code != 200 {
			t.Fatalf("entry %d: %s", w.Code, w.Body.String())
		}
	}
	payload, _, err := f.h.compileNodeRuntimeConfig(a, "connector", "0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	json.Unmarshal(payload, &config)
	groups := config["outbound_groups"].([]interface{})
	if len(groups) != 1 {
		t.Fatalf("duplicated pools: %v", groups)
	}
	outbounds := config["outbounds"].([]interface{})
	if len(outbounds) != 2 {
		t.Fatalf("duplicated members: %v", outbounds)
	}
	inbounds := config["inbounds"].([]interface{})
	if len(inbounds) != 3 {
		t.Fatalf("listeners %v", inbounds)
	}
	ports := []float64{1443, 2443, 1443}
	for i, inbound := range inbounds {
		p := inbound.(map[string]interface{})["protocol"].(map[string]interface{})
		if p["type"] != "direct" || p["port"] != ports[i] {
			t.Fatalf("A terminates B protocol or wrong parent: %v", p)
		}
	}
	rules := config["route"].(map[string]interface{})["rules"].([]interface{})
	for i, rule := range rules {
		action := rule.(map[string]interface{})["action"].(map[string]interface{})
		if i < 2 && action["outbound"] != fmt.Sprintf("pool-%d/auto", pool.ID) {
			t.Fatal("pool target mismatch")
		}
		if i == 2 && action["type"] != "direct" {
			t.Fatal("direct entry routed through pool")
		}
	}
	if strings.Contains(string(payload), "b-user-secret") {
		t.Fatal("B credential copied into A")
	}
	var stored model.NodeProxyPool
	f.h.db.First(&stored, pool.ID)
	if stored.Config == "" || strings.Contains(stored.Config, "pool-only-secret") {
		t.Fatal("pool stored without encryption")
	}
	w := poolRequest(t, f, "GET", fmt.Sprintf("/api/v1/admin/node-proxy-pools?node_id=%d", a.ID), nil, f.h.NodeProxyPoolsHandler)
	if strings.Contains(w.Body.String(), stored.Config) || strings.Contains(w.Body.String(), "pool-only-secret") {
		t.Fatal("pool list leaked credential")
	}
	// Full compiled document must be accepted by the actual Zero when available.
	if binary := os.Getenv("ZBOARD_ZERO_VALIDATE_BIN"); binary != "" {
		path := filepath.Join(t.TempDir(), "a.json")
		os.WriteFile(path, payload, 0600)
		if out, err := exec.Command(binary, "validate", path).CombinedOutput(); err != nil {
			t.Fatalf("A invalid: %v %s", err, out)
		}
	}
}
func TestSharedPoolReferencesAreNodeScopedAndProtected(t *testing.T) {
	poolValidationForTest(t)
	f, a, b := networkEntryFixture(t)
	pool := createPoolForTest(t, f, a.ID)
	w := poolRequest(t, f, "POST", "/api/v1/admin/network-entries", map[string]interface{}{"name": "bound", "node_id": a.ID, "parent_protocol_id": b.ID, "address": a.Address, "port": 12345, "enabled": true, "proxy_pool_id": pool.ID}, f.h.NetworkEntriesHandler)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = poolRequest(t, f, "DELETE", fmt.Sprintf("/api/v1/admin/node-proxy-pools/%d", pool.ID), nil, f.h.NodeProxyPoolsHandler)
	if w.Code != 400 {
		t.Fatal("deleted referenced pool")
	}
	if _, err := f.h.proxyPoolPath(f.h.db, pool.ID, b.NodeID, "tcp"); err == nil {
		t.Fatal("cross-node pool accepted")
	}
	w = poolRequest(t, f, "PUT", fmt.Sprintf("/api/v1/admin/node-proxy-pools/%d", pool.ID), map[string]interface{}{"node_id": a.ID, "name": "stale", "revision": 0}, f.h.NodeProxyPoolsHandler)
	if w.Code != 409 {
		t.Fatal("stale update accepted")
	}
	// A TCP-only replacement must roll back while a UDP entry references the pool.
	config := map[string]interface{}{"outbounds": []interface{}{map[string]interface{}{"tag": "http", "protocol": map[string]interface{}{"type": "http", "server": "192.0.2.1", "port": 8080}}}, "outbound_groups": []interface{}{}, "target": "http"}
	w = poolRequest(t, f, "PUT", fmt.Sprintf("/api/v1/admin/node-proxy-pools/%d", pool.ID), map[string]interface{}{"node_id": a.ID, "name": "incompatible", "revision": pool.Revision, "config": config}, f.h.NodeProxyPoolsHandler)
	if w.Code != 400 {
		t.Fatal("incompatible pool replacement accepted")
	}
	var stored model.NodeProxyPool
	f.h.db.First(&stored, pool.ID)
	if stored.Revision != pool.Revision || stored.Name != "shared" {
		t.Fatal("failed edit changed stored pool")
	}
	// Renaming without a config preserves encrypted credentials and queues only A.
	f.h.db.Exec("DELETE FROM node_config_publishes")
	w = poolRequest(t, f, "PUT", fmt.Sprintf("/api/v1/admin/node-proxy-pools/%d", pool.ID), map[string]interface{}{"node_id": a.ID, "name": "renamed", "revision": pool.Revision}, f.h.NodeProxyPoolsHandler)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var publishes []model.NodeConfigPublish
	f.h.db.Find(&publishes)
	if len(publishes) != 1 || publishes[0].NodeID != a.ID {
		t.Fatalf("pool update published to B: %v", publishes)
	}
}

func TestProxyPoolSubscriptionParsesZeroAndClashWithoutImportingRoutes(t *testing.T) {
	zeroDocument := `{"outbounds":[{"tag":"direct","protocol":{"type":"direct"}},{"tag":"hk","protocol":{"type":"shadowsocks","server":"198.51.100.1","port":443,"cipher":"aes-128-gcm","password":"secret"}}],"outbound_groups":[{"tag":"best","type":"url_test","outbounds":["hk"],"interval_seconds":300}],"route":{"rules":[{"condition":{"type":"domain"}}],"final":{"type":"route","outbound":"best"}}}`
	encoded := base64.RawStdEncoding.EncodeToString([]byte(zeroDocument))
	path, count, err := parseProxyPoolSubscription([]byte(encoded), "auto")
	if err != nil || count != 1 || path.Target != "best" || len(path.Outbounds) != 2 || len(path.Groups) != 1 {
		t.Fatalf("zero subscription = %#v, count=%d, error=%v", path, count, err)
	}

	clash := `
proxies:
  - name: hk
    type: ss
    server: 198.51.100.2
    port: 443
    cipher: aes-128-gcm
    password: secret
proxy-groups:
  - name: best
    type: url-test
    proxies: [hk, DIRECT]
    interval: 600
rules:
  - MATCH,best
`
	path, count, err = parseProxyPoolSubscription([]byte(clash), "clash")
	if err != nil || count != 1 || path.Target != "best" || len(path.Groups) != 1 {
		t.Fatalf("clash subscription = %#v, count=%d, error=%v", path, count, err)
	}
	if _, exists := path.Groups[0]["rules"]; exists {
		t.Fatal("subscription routes entered proxy pool graph")
	}
}

func TestProxyPoolSubscriptionIsEncryptedAndManualRawOverwriteIsPreserved(t *testing.T) {
	poolValidationForTest(t)
	f, a, _ := networkEntryFixture(t)
	subscriptionURL := "https://example.com/subscription/private-token"
	first := base64.StdEncoding.EncodeToString([]byte(`{"outbounds":[{"tag":"remote","protocol":{"type":"shadowsocks","server":"198.51.100.10","port":443,"cipher":"aes-128-gcm","password":"remote-secret"}}],"target":"remote"}`))
	stubProxyPoolSubscriptionFetch(t, first, nil)
	w := poolRequest(t, f, "POST", "/api/v1/admin/node-proxy-pools", map[string]interface{}{
		"node_id": a.ID, "name": "subscribed", "subscription_url": subscriptionURL,
		"subscription_format": "auto", "subscription_user_agent": "test-client/1", "auto_sync": true, "sync_interval_seconds": 3600,
	}, f.h.NodeProxyPoolsHandler)
	if w.Code != http.StatusOK {
		t.Fatalf("create subscribed pool: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "private-token") || strings.Contains(w.Body.String(), "remote-secret") {
		t.Fatal("mutation response leaked subscription or proxy credentials")
	}
	var stored model.NodeProxyPool
	if err := f.h.db.Where("name = ?", "subscribed").First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.SubscriptionURL == subscriptionURL || !stored.AutoSync || stored.SubscriptionNodeCount != 1 || stored.LastSyncAt == nil {
		t.Fatalf("subscription metadata was not securely persisted: %#v", stored)
	}
	decryptedURL, err := f.h.credentialCipher.Decrypt(stored.SubscriptionURL)
	if err != nil || decryptedURL != subscriptionURL {
		t.Fatalf("decrypt subscription URL: %q %v", decryptedURL, err)
	}

	detailPath := fmt.Sprintf("/api/v1/admin/node-proxy-pools/%d/config", stored.ID)
	w = poolRequest(t, f, "GET", detailPath, nil, f.h.NodeProxyPoolConfigHandler)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), subscriptionURL) {
		t.Fatalf("audited config read did not return source: %d %s", w.Code, w.Body.String())
	}
	list := poolRequest(t, f, "GET", fmt.Sprintf("/api/v1/admin/node-proxy-pools?node_id=%d", a.ID), nil, f.h.NodeProxyPoolsHandler)
	if strings.Contains(list.Body.String(), "private-token") || !strings.Contains(list.Body.String(), `"subscription_configured":true`) {
		t.Fatalf("pool list source projection is unsafe or incomplete: %s", list.Body.String())
	}

	manual := map[string]interface{}{"outbounds": []interface{}{map[string]interface{}{"tag": "manual", "protocol": map[string]interface{}{"type": "shadowsocks", "server": "198.51.100.20", "port": 443, "cipher": "aes-128-gcm", "password": "manual-secret"}}}, "outbound_groups": []interface{}{}, "target": "manual"}
	w = poolRequest(t, f, "PUT", fmt.Sprintf("/api/v1/admin/node-proxy-pools/%d", stored.ID), map[string]interface{}{"node_id": a.ID, "name": stored.Name, "revision": stored.Revision, "config": manual}, f.h.NodeProxyPoolsHandler)
	if w.Code != http.StatusOK {
		t.Fatalf("manual overwrite: %d %s", w.Code, w.Body.String())
	}
	if err := f.h.db.First(&stored, stored.ID).Error; err != nil {
		t.Fatal(err)
	}
	decryptedURL, _ = f.h.credentialCipher.Decrypt(stored.SubscriptionURL)
	decryptedConfig, _ := f.h.credentialCipher.Decrypt(stored.Config)
	if decryptedURL != subscriptionURL || !strings.Contains(decryptedConfig, "manual-secret") {
		t.Fatal("manual RAW overwrite removed the subscription or failed to replace the pool")
	}
}

func TestProxyPoolSubscriptionSyncKeepsLastGoodConfigOnFailure(t *testing.T) {
	poolValidationForTest(t)
	f, a, _ := networkEntryFixture(t)
	pool := createPoolForTest(t, f, a.ID)
	encryptedURL, err := f.h.credentialCipher.Encrypt("https://example.com/subscription/token")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&pool).Updates(map[string]interface{}{"subscription_url": encryptedURL, "subscription_format": "zero", "sync_interval_seconds": 3600}).Error; err != nil {
		t.Fatal(err)
	}
	var before model.NodeProxyPool
	f.h.db.First(&before, pool.ID)
	stubProxyPoolSubscriptionFetch(t, "", errors.New("订阅请求失败，请检查地址、网络和 TLS"))
	expected := before.Revision
	if _, err := f.h.syncNodeProxyPoolSubscription(context.Background(), pool.ID, &expected, &authClaims{UserID: 1, Email: "admin@example.test"}); err == nil {
		t.Fatal("failed subscription sync succeeded")
	}
	var after model.NodeProxyPool
	f.h.db.First(&after, pool.ID)
	if after.Config != before.Config || after.Revision != before.Revision || after.LastSyncError == "" {
		t.Fatal("failed sync replaced the last good pool or omitted failure status")
	}
}

func TestForwardServiceMembershipChangesAreAtomicAndQueueCredentialReconcile(t *testing.T) {
	f, a, b := networkEntryFixture(t)
	entry := saveEntryForTest(t, f, a, b, "")
	groups := []model.NodeGroup{{Name: "one", Code: "one", Revision: 1, IsEnabled: true}, {Name: "two", Code: "two", Revision: 1, IsEnabled: true}}
	for i := range groups {
		if err := f.h.db.Create(&groups[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	var tasks []model.Task
	apply := func(changes []protocolEndpointNodeGroupMembershipChange) error {
		return f.h.db.Transaction(func(tx *gorm.DB) error {
			return f.h.applyNetworkEntryMembershipChanges(tx, entry, changes, authClaims{UserID: 1, Email: "admin@example.test"}, &tasks)
		})
	}
	if err := apply([]protocolEndpointNodeGroupMembershipChange{{NodeGroupID: groups[0].ID, ExpectedRevision: 1, Member: true}, {NodeGroupID: groups[1].ID, ExpectedRevision: 99, Member: true}}); err == nil {
		t.Fatal("stale group accepted")
	}
	var count int64
	f.h.db.Model(&model.NodeGroupNetworkEntry{}).Count(&count)
	if count != 0 {
		t.Fatal("partial group assignment committed")
	}
	f.h.db.Model(&model.Task{}).Count(&count)
	if count != 0 {
		t.Fatal("failed group assignment created task")
	}
	tasks = nil
	if err := apply([]protocolEndpointNodeGroupMembershipChange{{NodeGroupID: groups[0].ID, ExpectedRevision: 1, Member: true}}); err != nil {
		t.Fatal(err)
	}
	var group model.NodeGroup
	f.h.db.First(&group, groups[0].ID)
	if group.Revision != 2 || len(tasks) != 1 || tasks[0].IdempotencyKey != fmt.Sprintf("node-group-reconcile:%d:2", group.ID) {
		t.Fatalf("revision/task mismatch: %v %v", group, tasks)
	}
	memberships, err := loadNetworkEntryMemberships(f.h.db, entry.ID)
	if err != nil || len(memberships) != 1 || memberships[0].NodeGroupID != group.ID {
		t.Fatal("forward grant not readable")
	}
	f.h.db.Model(&model.NodeGroupEndpoint{}).Where("node_group_id = ?", group.ID).Count(&count)
	if count != 0 {
		t.Fatal("grant added B direct access")
	}
}
