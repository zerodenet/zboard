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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/plugins"
)

func actionErrorPackage(t *testing.T) ([]byte, map[string]string) {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "plugin")
	if output, err := exec.Command("go", "build", "-o", binary, "../plugins/testdata/action-errors").CombinedOutput(); err != nil {
		t.Fatalf("build generic action fixture: %v %s", err, output)
	}
	native, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	ui, err := os.ReadFile("../plugins/testdata/action-errors/index.html")
	if err != nil {
		t.Fatal(err)
	}
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	runtimePath := "runtimes/host/plugin"
	m := plugins.Manifest{SchemaVersion: 1, ID: "example.action-errors", Name: "Generic action errors", Version: "1.0.0", Requires: plugins.Requirements{ZBoard: ">=0.0.1 <0.1.0", Tested: []string{"0.0.1"}, Protocol: 1, Bridge: 1}, Capabilities: []string{plugins.PageCapability, plugins.UISlotCapability, plugins.ConfigCapability}, Surfaces: []string{"account"}, Components: plugins.Components{UI: map[string]string{"account": "ui/index.html"}, Server: &struct {
		Executables map[string]string `json:"executables"`
	}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: runtimePath}}}}
	m.Contributions.Pages = []plugins.Page{{ID: "actions", Surface: "account", Purpose: "business", Title: "Actions"}}
	m.Contributions.Slots = []plugins.Slot{{ID: "actions", Surface: "account", Slot: "account.subscriptions.actions", Title: "Actions", Entrypoint: "ui/index.html"}}
	files := map[string][]byte{runtimePath: native, "ui/index.html": ui}
	m.Files = map[string]string{}
	for name, value := range files {
		sum := sha256.Sum256(value)
		m.Files[name] = hex.EncodeToString(sum[:])
	}
	files["manifest.json"], _ = json.Marshal(m)
	files["signature.json"], _ = json.Marshal(plugins.Signature{Algorithm: "ed25519", KeyID: "action.tests", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, files["manifest.json"]))})
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	for name, value := range files {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write(value); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes(), map[string]string{"action.tests": base64.StdEncoding.EncodeToString(public)}
}
