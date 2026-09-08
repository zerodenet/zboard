package plugins

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRealPluginProcessConfigurationAndStop(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "plugin")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", binary, "./testdata/server")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build plugin: %v %s", err, out)
	}
	payload, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	raw, keys := fixturePackage(t, func(m *Manifest, files map[string][]byte) {
		m.ID = "example.server"
		m.Capabilities = []string{"zboard.config.v1"}
		m.Surfaces = nil
		m.Contributions.Pages = nil
		m.Components.UI = nil
		m.Components.Server = &struct {
			Executables map[string]string `json:"executables"`
		}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "runtimes/host/plugin"}}
		files["runtimes/host/plugin"] = payload
	})
	m, db, _ := testManager(t, keys)
	v, err := importApproved(t, m, raw)
	if err != nil {
		t.Fatal(err)
	}
	v, err = m.Action(context.Background(), v.ID, "enable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	p := m.processes[v.ID]
	if p == nil || p.client.Exited() {
		t.Fatal("plugin process is not running")
	}
	if _, err := m.SaveConfig(context.Background(), v.ID, "admin", 0, []byte(`{"healthy":true}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveConfig(context.Background(), v.ID, "admin", 1, []byte(`{"reject":true}`)); err == nil {
		t.Fatal("plugin config rejection ignored")
	}
	if err := m.TestConfig(context.Background(), v.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_live_config BEFORE UPDATE OF config_ciphertext ON plugin_installations BEGIN SELECT RAISE(ABORT, 'injected failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveConfig(context.Background(), v.ID, "admin", 1, []byte(`{"healthy":false}`)); err == nil {
		t.Fatal("failed persistence was reported as success")
	}
	if err := m.TestConfig(context.Background(), v.ID, "admin"); err != nil {
		t.Fatal("live process did not restore old configuration", err)
	}
	if err := db.Exec(`DROP TRIGGER fail_live_config`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := m.Action(context.Background(), v.ID, "disable", "admin", v.Generation, false, ""); err != nil {
		t.Fatal(err)
	}
	if !p.client.Exited() {
		t.Fatal("plugin process survived disable")
	}
}
