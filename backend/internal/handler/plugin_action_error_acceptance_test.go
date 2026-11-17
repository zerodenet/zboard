package handler

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/plugins"
)

func enabledActionErrorFixture(t *testing.T) *pluginLifecycleFixture {
	t.Helper()
	archive, keys := actionErrorPackage(t)
	f := newPluginLifecycleFixture(t, archive, keys)
	var err error
	f.installation, err = f.manager.Action(t.Context(), f.installation.ID, "enable", "admin", f.installation.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPluginActionErrorsUseRealNativeAndHTTPContract(t *testing.T) {
	f := enabledActionErrorFixture(t)
	code, raw := f.request(t, "POST", "/api/v1/plugin-ui/example.action-errors/session", f.memberToken, `{"page":"actions","surface":"account"}`)
	var session struct {
		Data plugins.Session `json:"data"`
	}
	if code != 200 || json.Unmarshal(raw, &session) != nil {
		t.Fatalf("session: %d %s", code, raw)
	}
	bridge := func(s plugins.Session, token, action string) (int, APIResponse) {
		r := announcementRequest("POST", f.server.URL+"/api/v1/plugin-ui/bridge", token, `{"type":"page.call","action":"`+action+`","payload":{"note":"keep input"}}`)
		r.RequestURI = ""
		r.Header.Set("X-Plugin-Session", s.Token)
		response, err := f.server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var body APIResponse
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, body
	}
	for _, test := range []struct {
		action, reason, message string
		retry                   bool
	}{
		{"reject.retry", "resource_busy", "Resource is busy. Please try later.", true},
		{"reject.permanent", "operation_closed", "此操作已关闭，请保留输入并联系管理员。", false},
		{"reject.html", "public_text", `<img src=x onerror="alert('unsafe')"> is plain text.`, false},
		{"reject.namespace", "session_expired", "Plugin-specific reason, not host authentication.", true},
	} {
		code, body := bridge(session.Data, f.memberToken, test.action)
		if code != 422 || body.Message != test.message || body.Error == nil || body.Error.Source != "plugin" || body.Error.Code != "plugin_action_rejected" || body.Error.PluginCode != test.reason || body.Error.Retryable == nil || *body.Error.Retryable != test.retry || body.Data != nil {
			t.Fatalf("%s: %d %+v", test.action, code, body)
		}
	}
	for _, action := range []string{"reject.long", "reject.malformed", "reject.legacy"} {
		code, body := bridge(session.Data, f.memberToken, action)
		if code != 503 || body.Error == nil || body.Error.Code != "unavailable" || body.Error.Source != "" || strings.Contains(body.Message, "private") || strings.Contains(body.Message, "secret") {
			t.Fatalf("unsafe downgrade %s: %d %+v", action, code, body)
		}
	}
	if code, body := bridge(session.Data, f.memberToken, "reject.timeout"); code != 504 || body.Error.Code != "deadline_exceeded" {
		t.Fatalf("timeout: %d %+v", code, body)
	}
	if code, body := bridge(session.Data, f.adminToken, "reject.retry"); code != 403 || body.Error.Code != "session_expired" {
		t.Fatalf("cross actor: %d %+v", code, body)
	}
	if code, body := bridge(session.Data, f.memberToken, "success.echo"); code != 200 || body.Data == nil {
		t.Fatalf("legacy success: %d %+v", code, body)
	}
	code, raw = f.request(t, "POST", "/api/v1/plugin-ui/example.action-errors/slot-session", f.memberToken, `{"id":"actions","slot":"account.subscriptions.actions","surface":"account","target_user_id":999}`)
	var slot struct {
		Data plugins.Session `json:"data"`
	}
	if code != 200 || json.Unmarshal(raw, &slot) != nil {
		t.Fatalf("slot: %d %s", code, raw)
	}
	if code, body := bridge(slot.Data, f.memberToken, "reject.retry"); code != 422 || body.Error.PluginCode != "resource_busy" {
		t.Fatalf("slot rejection: %d %+v", code, body)
	}
	revoke := announcementRequest("DELETE", f.server.URL+"/api/v1/plugin-ui/session", f.memberToken, "")
	revoke.RequestURI = ""
	revoke.Header.Set("X-Plugin-Session", session.Data.Token)
	revoked, err := f.server.Client().Do(revoke)
	if err != nil {
		t.Fatal(err)
	}
	revoked.Body.Close()
	if revoked.StatusCode != 200 {
		t.Fatalf("revoke failed: %d", revoked.StatusCode)
	}
	if code, body := bridge(session.Data, f.memberToken, "reject.retry"); code != 403 || body.Error.Code != "session_expired" {
		t.Fatalf("revoked: %d %+v", code, body)
	}
	if code, body := bridge(slot.Data, f.memberToken, "reject.exit"); code != 503 && code != 403 || body.Error == nil || body.Error.Source == "plugin" {
		t.Fatalf("process exit: %d %+v", code, body)
	}
}

func TestPluginActionErrorsFullHTTPUIBridge(t *testing.T) {
	if os.Getenv("ZBOARD_RUN_ACTION_UI_TESTS") != "1" {
		t.Skip("opt-in real HTTP/native/UI acceptance")
	}
	f := enabledActionErrorFixture(t)
	root, err := filepath.Abs("../../../frontend")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "node", filepath.Join(root, "node_modules/vitest/vitest.mjs"), "run", "src/plugins/PluginActionHTTP.test.ts", "--maxWorkers=1", "--minWorkers=1")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "ZBOARD_ACTION_ACCEPTANCE_URL="+f.server.URL, "ZBOARD_ACTION_ACCEPTANCE_TOKEN="+f.memberToken)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("full HTTP UI chain: %v\n%s", err, output)
	} else {
		t.Log(string(output))
	}
}

func TestPluginActionErrorBrowserFixture(t *testing.T) {
	ready := os.Getenv("ZBOARD_ACTION_BROWSER_READY_FILE")
	if ready == "" {
		t.Skip("opt-in real browser fixture")
	}
	f := enabledActionErrorFixture(t)
	raw, _ := json.Marshal(map[string]string{"url": f.server.URL, "plugin_id": f.installation.ID, "token": f.memberToken})
	if err := os.WriteFile(ready, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("Generic action browser fixture ready")
	for {
		if _, err := os.Stat(ready + ".stop"); err == nil {
			return
		}
		select {
		case <-t.Context().Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}
