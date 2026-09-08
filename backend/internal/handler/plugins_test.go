package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
)

func TestPluginHTTPBoundaryAndRevokedAssets(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	source := "../../../examples/plugins/welcome"
	raw, err := os.ReadFile(filepath.Join(source, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest plugins.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Files = map[string]string{}
	files := map[string][]byte{}
	for _, name := range []string{"ui/index.html", "ui/style.css", "ui/bridge.js"} {
		b, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = b
		sum := sha256.Sum256(b)
		manifest.Files[name] = hex.EncodeToString(sum[:])
	}
	files["manifest.json"], _ = json.Marshal(manifest)
	files["signature.json"], _ = json.Marshal(plugins.Signature{Algorithm: "ed25519", KeyID: "example", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, files["manifest.json"]))})
	var buffer bytes.Buffer
	zw := zip.NewWriter(&buffer)
	for name, b := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	manager, err := plugins.NewManager(h.db, newTestCredentialCipher(t), plugins.Options{Directory: t.TempDir(), TrustedPublishers: map[string]string{"example": base64.StdEncoding.EncodeToString(pub)}}, "v0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	h.SetPluginManager(manager)
	for _, handler := range []http.HandlerFunc{h.AdminPluginsHandler, h.AdminPluginMarketHandler, h.AdminPluginActionHandler, h.AdminPluginConfigHandler, h.AdminPluginTestHandler, h.AdminPluginOperationsHandler, h.AdminPluginMigrationsHandler} {
		response := httptest.NewRecorder()
		handler(response, announcementRequest("POST", "/", token, "{}"))
		if response.Code != 403 {
			t.Fatalf("non-admin access: %d %s", response.Code, response.Body)
		}
	}
	v, err := manager.Import(buffer.Bytes(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	v, err = manager.Action(context.Background(), v.ID, "enable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	s, err := manager.CreateSession(v.ID, "welcome", "public", 0, false, false)
	if err != nil {
		t.Fatal(err)
	}
	asset := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.PluginAssetHandler(w, httptest.NewRequest("GET", strings.Split(s.URL, "#")[0], nil))
		return w
	}
	w := asset()
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "connect-src 'none'") || strings.Contains(w.Header().Get("Content-Security-Policy"), "allow-same-origin") {
		t.Fatal(w.Code, w.Header())
	}
	for _, typ := range []string{"users.credentials.rotate", "core.command", "config.save"} {
		r := httptest.NewRequest("POST", "/api/v1/plugin-ui/bridge", strings.NewReader(`{"type":"`+typ+`"}`))
		r.Header.Set("X-Plugin-Session", s.Token)
		w := httptest.NewRecorder()
		h.PluginBridgeHandler(w, r)
		if w.Code != 403 {
			t.Fatal("core capability escaped", w.Code, w.Body)
		}
	}
	r := announcementRequest("POST", "/api/v1/plugin-ui/bridge", token, `{"type":"context.load"}`)
	r.Header.Set("X-Plugin-Session", s.Token)
	w = httptest.NewRecorder()
	h.PluginBridgeHandler(w, r)
	if w.Code != 403 {
		t.Fatal("session crossed identities")
	}
	if _, err := manager.Action(context.Background(), v.ID, "disable", "admin", v.Generation, false, ""); err != nil {
		t.Fatal(err)
	}
	if w := asset(); w.Code != 404 {
		t.Fatal("disabled plugin asset still served", w.Code)
	}
	var users int64
	h.db.Model(&model.User{}).Count(&users)
	if users != 1 {
		t.Fatal("core state changed")
	}
}

func TestUnavailablePluginRuntimeDoesNotDispatchOrPanic(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	called := false
	w := httptest.NewRecorder()
	h.PluginGuard(func(http.ResponseWriter, *http.Request) { called = true })(w, httptest.NewRequest("GET", "/api/v1/admin/plugins", nil))
	if w.Code != 503 || called {
		t.Fatal("unavailable runtime was dispatched", w.Code)
	}
}
