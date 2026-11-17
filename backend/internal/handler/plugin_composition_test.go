package handler

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

func pluginCompositionHandlers(t *testing.T) (*handlers, string, *plugins.Manager, *fakeIdentityRuntime) {
	t.Helper()
	h, token, identityRuntime := identityTestHandlers(t)
	raw, err := os.ReadFile("../../../examples/plugins/welcome/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest plugins.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.ID = "test.oauth"
	manifest.Name = "企业登录"
	manifest.Capabilities = []string{plugins.PageCapability, plugins.ConfigCapability, plugins.IdentityCapability, plugins.UISlotCapability}
	manifest.Surfaces = []string{"public", "account", "admin"}
	manifest.Contributions.Pages = nil
	manifest.Contributions.Slots = []plugins.Slot{{ID: "login", Surface: "public", Slot: "auth.login.methods", Title: "企业登录", Entrypoint: "ui/index.html"}, {ID: "private", Surface: "admin", Slot: "admin.user.actions", Title: "用户操作", Entrypoint: "ui/index.html"}}
	manifest.Components.Server = &struct {
		Executables map[string]string `json:"executables"`
	}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "runtimes/host/plugin"}}
	files := map[string][]byte{"ui/index.html": []byte("<p>isolated fixture</p>"), "runtimes/host/plugin": []byte("signed fixture; never executed")}
	// Retain assets referenced by the example's UI declaration.
	for _, name := range []string{"ui/style.css", "ui/bridge.js"} {
		value, err := os.ReadFile("../../../examples/plugins/welcome/" + name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = value
	}
	manifest.Files = map[string]string{}
	for name, value := range files {
		sum := sha256.Sum256(value)
		manifest.Files[name] = hex.EncodeToString(sum[:])
	}
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	files["manifest.json"], _ = json.Marshal(manifest)
	files["signature.json"], _ = json.Marshal(plugins.Signature{Algorithm: "ed25519", KeyID: "test", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, files["manifest.json"]))})
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, value := range files {
		w, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(value); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	manager, err := plugins.NewManager(h.db, newTestCredentialCipher(t), plugins.Options{Directory: t.TempDir(), TrustedPublishers: map[string]string{"test": base64.StdEncoding.EncodeToString(pub)}}, "v0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	v, err := manager.Import(buffer.Bytes(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&model.PluginInstallation{}).Where("id=?", v.ID).Updates(map[string]any{"enabled": true, "state": "active", "config_revision": 1}).Error; err != nil {
		t.Fatal(err)
	}
	identityRuntime.snapshot.Generation = v.Generation
	h.SetPluginManager(manager)
	h.identityProviders = identityRuntime
	return h, token, manager, identityRuntime
}
func TestPluginCompositionPublicMetadataAndPrivateScope(t *testing.T) {
	h, token, _, _ := pluginCompositionHandlers(t)
	for _, surface := range []string{"public", "admin", "account"} {
		req := announcementRequest("GET", "/api/v1/plugin-ui/composition?surface="+surface, "", "")
		// Public presentation does not use or renew an expired account credential.
		if surface == "public" {
			req.Header.Set("Authorization", "Bearer expired")
		}
		w := httptest.NewRecorder()
		h.PluginCompositionHandler(w, req)
		if surface != "public" {
			if w.Code != 401 {
				t.Fatal(surface, w.Code, w.Body.String())
			}
			continue
		}
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"availability":"unavailable"`) || !strings.Contains(w.Body.String(), `"presentation":"identity"`) {
			t.Fatal(w.Code, w.Body.String())
		}
		for _, secret := range []string{"bridge_token", "client_secret", "config_ciphertext", "用户操作"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("private data leaked", secret)
			}
		}
	}
	w := httptest.NewRecorder()
	h.PluginCompositionHandler(w, announcementRequest("GET", "/api/v1/plugin-ui/composition?surface=admin", token, ""))
	if w.Code != 403 {
		t.Fatal("member admin snapshot", w.Code, w.Body.String())
	}
}
func TestPluginNativeIdentityStartKeepsAdmissionOriginPKCEAndRevisionFence(t *testing.T) {
	h, _, _, identityRuntime := pluginCompositionHandlers(t)
	call := func(plugin, body, origin string) *httptest.ResponseRecorder {
		req := announcementRequest("POST", "https://panel.example.test/api/v1/plugin-ui/"+plugin+"/identity-start", "", body)
		req.Header.Set("Origin", origin)
		req = pathvar.WithVars(req, map[string]string{"id": plugin})
		w := httptest.NewRecorder()
		h.PluginIdentityStartHandler(w, req)
		return w
	}
	body := `{"slot_id":"login","provider_id":"test.oauth","generation":1,"config_revision":1}`
	var request map[string]any
	json.Unmarshal([]byte(body), &request)
	request["generation"] = identityRuntime.snapshot.Generation
	bytesBody, _ := json.Marshal(request)
	body = string(bytesBody)
	w := call("test.oauth", body, "https://panel.example.test")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Data struct {
			URL string `json:"authorization_url"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse(response.Data.URL)
	q := target.Query()
	if q.Get("code_challenge_method") != "S256" || len(q.Get("state")) != 43 || len(q.Get("nonce")) != 43 {
		t.Fatal(target)
	}
	if len(w.Result().Cookies()) != 2 || !w.Result().Cookies()[0].HttpOnly || !w.Result().Cookies()[0].Secure {
		t.Fatal(w.Result().Cookies())
	}
	if v := call("test.oauth", body, "https://evil.example"); v.Code != 403 {
		t.Fatal(v.Code, v.Body.String())
	}
	if v := call("test.oauth", strings.Replace(body, "test.oauth", "other.provider", 1), "https://panel.example.test"); v.Code != 403 {
		t.Fatal(v.Code, v.Body.String())
	}
	identityRuntime.snapshot.Revision++
	if v := call("test.oauth", body, "https://panel.example.test"); v.Code != 409 {
		t.Fatal("stale display started a new configuration", v.Code, v.Body.String())
	}
	h.db.Model(&model.PluginInstallation{}).Where("id=?", "test.oauth").Update("enabled", false)
	if v := call("test.oauth", body, "https://panel.example.test"); v.Code != 403 {
		t.Fatal("disabled plugin started login", v.Code, v.Body.String())
	}
}
