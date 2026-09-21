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
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"github.com/zerodenet/zboard/backend/internal/security"
)

func configBridgePackage(t *testing.T) ([]byte, map[string]string) {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "plugin")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	command := exec.Command("go", "build", "-ldflags", "-X main.pluginCapabilities=zboard.config.v1,zboard.ui.page.v1", "-o", binary, "../plugins/testdata/server")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build configuration plugin: %v %s", err, output)
	}
	runtimeBytes, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	runtimePath := "runtimes/" + runtime.GOOS + "-" + runtime.GOARCH + "/plugin"
	manifest := plugins.Manifest{
		SchemaVersion: 1, ID: "example.server", Name: "Configuration bridge", Version: "1.0.0",
		Requires:     plugins.Requirements{ZBoard: ">=0.0.1 <0.1.0", Tested: []string{"0.0.1"}, Protocol: 1, Bridge: 1},
		Capabilities: []string{plugins.ConfigCapability, plugins.PageCapability}, Surfaces: []string{"admin"},
		Components: plugins.Components{UI: map[string]string{"admin": "ui/admin.html"}, Server: &struct {
			Executables map[string]string `json:"executables"`
		}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: runtimePath}}},
	}
	manifest.Contributions.Pages = []plugins.Page{{ID: "settings", Surface: "admin", Purpose: "configuration", Title: "Settings"}}
	files := map[string][]byte{runtimePath: runtimeBytes, "ui/admin.html": []byte("<!doctype html><title>Settings</title>")}
	manifest.Files = map[string]string{}
	for name, data := range files {
		digest := sha256.Sum256(data)
		manifest.Files[name] = hex.EncodeToString(digest[:])
	}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signatureRaw, err := json.Marshal(plugins.Signature{
		PublicKey: base64.StdEncoding.EncodeToString(public), Algorithm: "ed25519", KeyID: "test.publisher",
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, manifestRaw)),
	})
	if err != nil {
		t.Fatal(err)
	}
	files["manifest.json"] = manifestRaw
	files["signature.json"] = signatureRaw
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, data := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes(), map[string]string{"test.publisher": base64.StdEncoding.EncodeToString(public)}
}

func TestConfigurationPageBridgePersistsAndRestartReappliesConfig(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	if err := h.db.Model(&model.User{}).Where("id = ?", 1).Updates(map[string]any{"is_admin": true, "email": "admin@example.test"}).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := h.issueToken(authClaims{UserID: 1, Email: "admin@example.test", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	archive, keys := configBridgePackage(t)
	cipher, err := security.NewCredentialCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	options := plugins.Options{Directory: t.TempDir(), TrustedPublishers: keys}
	manager, err := plugins.NewManager(h.db, cipher, options, "v0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	h.SetPluginManager(manager)
	installation, err := manager.Import(archive, "admin")
	if err != nil {
		manager.Close()
		t.Fatal(err)
	}
	session, err := manager.CreateSession(installation.ID, "settings", "admin", 1, true, true)
	if err != nil {
		manager.Close()
		t.Fatal(err)
	}
	bridge := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/plugin-ui/bridge", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Plugin-Session", session.Token)
		response := httptest.NewRecorder()
		h.PluginBridgeHandler(response, request)
		return response
	}
	saved := bridge(`{"type":"config.save","revision":0,"config":{"healthy":true,"provider":"https://example.test"}}`)
	if saved.Code != http.StatusOK || !strings.Contains(saved.Body.String(), `"revision":1`) {
		manager.Close()
		t.Fatalf("config.save status=%d body=%s", saved.Code, saved.Body.String())
	}
	loaded := bridge(`{"type":"config.load"}`)
	if loaded.Code != http.StatusOK || !strings.Contains(loaded.Body.String(), `"provider":"https://example.test"`) {
		manager.Close()
		t.Fatalf("config.load status=%d body=%s", loaded.Code, loaded.Body.String())
	}
	var stored model.PluginInstallation
	if err := h.db.First(&stored, "id = ?", installation.ID).Error; err != nil || stored.ConfigRevision != 1 || stored.ConfigCiphertext == "" || strings.Contains(stored.ConfigCiphertext, "example.test") {
		manager.Close()
		t.Fatalf("encrypted config row=%+v err=%v", stored, err)
	}
	installation, err = manager.Action(context.Background(), installation.ID, "enable", "admin", installation.Generation, false, "")
	if err != nil {
		manager.Close()
		t.Fatal(err)
	}
	manager.Close()

	restarted, err := plugins.NewManager(h.db, cipher, options, "v0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	h.SetPluginManager(restarted)
	if err := restarted.TestConfig(context.Background(), installation.ID, "admin"); err != nil {
		t.Fatalf("restart did not reapply committed config: %v", err)
	}
	reopenedSession, err := restarted.CreateSession(installation.ID, "settings", "admin", 1, true, true)
	if err != nil {
		t.Fatal(err)
	}
	reopenedRequest := httptest.NewRequest(http.MethodPost, "/api/v1/plugin-ui/bridge", strings.NewReader(`{"type":"config.load"}`))
	reopenedRequest.Header.Set("Authorization", "Bearer "+token)
	reopenedRequest.Header.Set("Content-Type", "application/json")
	reopenedRequest.Header.Set("X-Plugin-Session", reopenedSession.Token)
	reopenedResponse := httptest.NewRecorder()
	h.PluginBridgeHandler(reopenedResponse, reopenedRequest)
	if reopenedResponse.Code != http.StatusOK || !strings.Contains(reopenedResponse.Body.String(), `"provider":"https://example.test"`) {
		t.Fatalf("reopened config page status=%d body=%s", reopenedResponse.Code, reopenedResponse.Body.String())
	}
	view, err := restarted.Config(installation.ID)
	if err != nil || !view.Configured || view.Revision != 1 || !bytes.Contains(view.Config, []byte(`"provider":"https://example.test"`)) {
		t.Fatalf("reopened config=%s revision=%d configured=%v err=%v", view.Config, view.Revision, view.Configured, err)
	}
}
