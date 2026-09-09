package plugins

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNativePluginStorageUsesScopedHostSDKAndClosesWithProcess(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "storage")
	cmd := exec.Command("go", "build", "-o", binary, "./testdata/storage")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	payload, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	keys := map[string]string{"test.publisher": base64.StdEncoding.EncodeToString(pub)}
	raw := fixtureSignedPackage(t, priv, pub, func(m *Manifest, files map[string][]byte) {
		m.ID = "example.storage"
		m.Capabilities = []string{ConfigCapability, StorageCapability}
		m.Surfaces = nil
		m.Contributions.Pages = nil
		m.Components.UI = nil
		m.Components.Server = &struct {
			Executables map[string]string `json:"executables"`
		}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "runtimes/host/plugin"}}
		m.Data = &DataManifest{Version: 1, MinCompatibleVersion: 1, Migrations: []DataMigration{{Version: 1}}}
		files["runtimes/host/plugin"] = payload
	})
	m, _, _ := testManager(t, keys)
	v, err := m.Import(raw, "admin")
	if err != nil {
		t.Fatal(err)
	}
	v, err = m.Action(context.Background(), v.ID, "enable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	p := m.processes[v.ID]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := p.api.TestConfig(ctx, &pluginv1.ConfigRequest{})
	if err != nil || !result.Healthy {
		t.Fatal("scoped native storage failed", err)
	}
	row, err := m.readData(v.ID)
	if err != nil {
		t.Fatal(err)
	}
	obj, err := m.decodeStorage(row)
	if err != nil || !strings.Contains(string(obj["background"]), "native-plugin") {
		t.Fatal("native write missing", err)
	}
	// Host-owned lifecycle RPCs cannot recursively mutate storage or deadlock.
	if err := m.TestConfig(ctx, v.ID, "admin"); err == nil {
		t.Fatal("storage allowed during lifecycle transaction")
	}
	// A failing candidate must leave the old process and state usable.
	pack, err := ReadPackage(raw, keys)
	if err != nil {
		t.Fatal(err)
	}
	replacement := func(version, description string) []byte {
		return fixtureSignedPackage(t, priv, pub, func(manifest *Manifest, files map[string][]byte) {
			*manifest = pack.Manifest
			manifest.Version, manifest.Description = version, description
			files["runtimes/host/plugin"] = payload
		})
	}
	if _, err = m.Import(replacement("2.0.0", "bad runtime identity"), "admin"); err == nil {
		t.Fatal("mismatched runtime installed")
	}
	current, _ := m.load(v.ID)
	if p.client.Exited() || m.processes[v.ID] != p || current.Digest != v.Digest || current.Generation != v.Generation {
		t.Fatal("failed runtime upgrade replaced working instance")
	}
	v, err = m.Import(replacement(pack.Manifest.Version, "compatible new package"), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !p.client.Exited() || m.processes[v.ID] == p || !v.Enabled || v.State != "active" {
		t.Fatal("upgrade did not swap process after commit")
	}
	p = m.processes[v.ID]
	row, _ = m.readData(v.ID)
	obj, err = m.decodeStorage(row)
	if err != nil || !strings.Contains(string(obj["background"]), "native-plugin") {
		t.Fatal("upgrade discarded data", err)
	}
	if _, err = m.Action(context.Background(), v.ID, "uninstall", "admin", v.Generation, false, ""); err != nil {
		t.Fatal(err)
	}
	if !p.client.Exited() {
		t.Fatal("uninstalled native process survived")
	}
}
