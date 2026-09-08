package plugins

import (
	"context"
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
	raw, keys := fixturePackage(t, func(m *Manifest, files map[string][]byte) {
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
	v, err = m.Authorize(v.ID, "admin", v.Digest, v.Generation, v.Manifest.Capabilities, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Action(context.Background(), v.ID, "migrate", "admin", v.Generation, false, ""); err == nil {
		t.Fatal("untrusted native program ran")
	}
	v, err = m.Authorize(v.ID, "admin", v.Digest, v.Generation, v.Manifest.Capabilities, true)
	if err != nil {
		t.Fatal(err)
	}
	v = migratePlugin(t, m, v)
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
	if _, err = m.Authorize(v.ID, "admin", v.Digest, v.Generation, []string{ConfigCapability}, true); err != nil {
		t.Fatal(err)
	}
	if !p.client.Exited() {
		t.Fatal("revoked native process survived")
	}
}
