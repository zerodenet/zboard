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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
)

func TestLegacyOfflineImportUsesPreviewConfirmationWithoutHostConfiguration(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	if err := h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := h.issueToken(authClaims{UserID: 1, Email: "admin@example.test", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	publicKey := base64.StdEncoding.EncodeToString(pub)
	root := "../../../examples/plugins/welcome"
	raw, err := os.ReadFile(filepath.Join(root, "manifest.json"))
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
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = data
		sum := sha256.Sum256(data)
		manifest.Files[name] = hex.EncodeToString(sum[:])
	}
	files["manifest.json"], _ = json.Marshal(manifest)
	// Existing releases omitted PublicKey; the dialog can supply it separately.
	files["signature.json"], _ = json.Marshal(plugins.Signature{Algorithm: "ed25519", KeyID: "example", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, files["manifest.json"]))})
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, data := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	manager, err := plugins.NewManager(h.db, newTestCredentialCipher(t), plugins.Options{Directory: t.TempDir()}, "v0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	h.SetPluginManager(manager)
	request := func(query, digest, fingerprint string) *httptest.ResponseRecorder {
		r := announcementRequest(http.MethodPost, "/api/v1/admin/plugins"+query, token, archive.String())
		r.Header.Set("Content-Type", "application/octet-stream")
		r.Header.Set("X-Plugin-Public-Key", publicKey)
		r.Header.Set("X-Plugin-Digest", digest)
		r.Header.Set("X-Plugin-Trust-Fingerprint", fingerprint)
		w := httptest.NewRecorder()
		h.AdminPluginsHandler(w, r)
		return w
	}
	w := request("?inspect=true", "", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var envelope struct {
		Data plugins.ImportPreview `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	preview := envelope.Data
	if preview.Trusted || preview.Fingerprint == "" {
		t.Fatal(preview)
	}
	var count int64
	h.db.Model(&model.PluginInstallation{}).Count(&count)
	if count != 0 {
		t.Fatal("preview installed package")
	}
	if w := request("", preview.Digest, ""); w.Code != 400 {
		t.Fatal("missing confirmation accepted", w.Code)
	}
	if w := request("", "stale", preview.Fingerprint); w.Code != 409 {
		t.Fatal("stale archive accepted", w.Code)
	}
	if w := request("", preview.Digest, preview.Fingerprint); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var installed model.PluginInstallation
	if err := h.db.First(&installed, "id = ?", manifest.ID).Error; err != nil {
		t.Fatal(err)
	}
	if installed.Enabled || !installed.LocalTrust || installed.SigningKey != publicKey {
		t.Fatal(installed)
	}
}
