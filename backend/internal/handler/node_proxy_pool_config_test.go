package handler

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestProxyPoolConfigReadIsExplicitAuditedAndEditable(t *testing.T) {
	poolValidationForTest(t)
	f, a, _ := networkEntryFixture(t)
	pool := createPoolForTest(t, f, a.ID)
	path := fmt.Sprintf("/api/v1/admin/node-proxy-pools/%d/config", pool.ID)
	for _, token := range []string{"", f.token} {
		w := httptest.NewRecorder()
		f.h.NodeProxyPoolConfigHandler(w, announcementRequest("GET", path, token, ""))
		if w.Code != 401 && w.Code != 403 {
			t.Fatalf("unprivileged read status %d", w.Code)
		}
		if strings.Contains(w.Body.String(), "pool-only-secret") {
			t.Fatal("unauthorized secret exposure")
		}
	}
	w := poolRequest(t, f, "GET", path, nil, f.h.NodeProxyPoolConfigHandler)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("read status %d", w.Code)
	}
	var result struct {
		Data struct {
			Pool     model.NodeProxyPool
			Config   map[string]interface{}
			Compiled map[string]interface{}
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Pool.Revision != pool.Revision || result.Data.Pool.Config != "" {
		t.Fatal("invalid pool metadata")
	}
	if result.Data.Config["target"] != "auto" || result.Data.Compiled["target"] != fmt.Sprintf("pool-%d/auto", pool.ID) {
		t.Fatal("stored and compiled graphs not separated")
	}
	nodes := result.Data.Config["outbounds"].([]interface{})
	protocol := nodes[0].(map[string]interface{})["protocol"].(map[string]interface{})
	if protocol["password"] != "pool-only-secret" {
		t.Fatal("edit did not hydrate credentials")
	}
	var count int64
	if err := f.h.db.Table("audit_logs").Where("action = ?", "node_proxy_pool.config.read").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("missing read audit: %v count %d", err, count)
	}
	protocol["password"] = "updated-pool-secret"
	update := poolRequest(t, f, "PUT", fmt.Sprintf("/api/v1/admin/node-proxy-pools/%d", pool.ID), map[string]interface{}{"node_id": a.ID, "name": pool.Name, "revision": pool.Revision, "config": result.Data.Config}, f.h.NodeProxyPoolsHandler)
	if update.Code != 200 {
		t.Fatalf("update failed: %d", update.Code)
	}
	if strings.Contains(update.Body.String(), "updated-pool-secret") {
		t.Fatal("mutation leaked credential")
	}
	stale := poolRequest(t, f, "PUT", fmt.Sprintf("/api/v1/admin/node-proxy-pools/%d", pool.ID), map[string]interface{}{"node_id": a.ID, "name": pool.Name, "revision": pool.Revision, "config": result.Data.Config}, f.h.NodeProxyPoolsHandler)
	if stale.Code != 409 {
		t.Fatalf("stale update status %d", stale.Code)
	}
	var stored model.NodeProxyPool
	f.h.db.First(&stored, pool.ID)
	if strings.Contains(stored.Config, "updated-pool-secret") {
		t.Fatal("unencrypted storage")
	}
	raw, err := f.h.credentialCipher.Decrypt(stored.Config)
	if err != nil || !strings.Contains(raw, "updated-pool-secret") {
		t.Fatal("edited credential not saved")
	}
}

func TestProxyPoolRuntimeExtractsOnlyOwnedPublishedGraph(t *testing.T) {
	raw := `{"api":{"token":"api-secret"},"outbounds":[{"tag":"pool-1/a","protocol":{"type":"socks5","password":"own-secret"}},{"tag":"pool-10/a","protocol":{"password":"other-secret"}}],"outbound_groups":[{"tag":"pool-1/pick","type":"selector","outbounds":["pool-1/a"]}],"inbounds":[{"tag":"entry-3","protocol":{"type":"direct","target":"landing.example","port":443}},{"tag":"unrelated","protocol":{"type":"vless","password":"listener-secret"}}],"route":{"rules":[{"condition":{"type":"inbound","values":["entry-3"]},"action":{"type":"route","outbound":"pool-1/pick"}},{"condition":{"type":"inbound","values":["unrelated"]},"action":{"type":"route","outbound":"pool-10/a"}}]}}`
	snapshot, err := extractNodeProxyPoolRuntime("SSH banner\n"+poolRuntimeBegin+raw+poolRuntimeEnd+"\n", 1)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(snapshot.Config)
	for _, secret := range []string{"api-secret", "other-secret", "listener-secret", "pool-10/"} {
		if strings.Contains(string(payload), secret) {
			t.Fatal("unrelated data exposed")
		}
	}
	if !snapshot.Present || len(snapshot.SHA256) != 64 || !strings.Contains(string(payload), "own-secret") || !strings.Contains(string(payload), "entry-3") {
		t.Fatal("published pool snapshot incomplete")
	}
	absent, err := extractNodeProxyPoolRuntime(poolRuntimeBegin+raw+poolRuntimeEnd, 2)
	if err != nil || absent.Present {
		t.Fatal("absent pool incorrectly reported present")
	}
	for _, output := range []string{"", poolRuntimeBegin + "invalid" + poolRuntimeEnd, poolRuntimeBegin + strings.Repeat(" ", 8*1024*1024+1) + poolRuntimeEnd} {
		if _, err := extractNodeProxyPoolRuntime(output, 1); err == nil {
			t.Fatal("invalid snapshot accepted")
		}
	}
}

func TestProxyPoolRuntimeReadRequiresAdminBeforeSSH(t *testing.T) {
	f, _, _ := networkEntryFixture(t)
	w := httptest.NewRecorder()
	f.h.NodeProxyPoolRuntimeHandler(w, announcementRequest("GET", "/api/v1/admin/node-proxy-pools/1/runtime", f.token, ""))
	if w.Code != 403 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unauthorized runtime read %d", w.Code)
	}
}
