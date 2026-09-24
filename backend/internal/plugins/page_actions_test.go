package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

type pageActionTestHostServices struct{}

func (pageActionTestHostServices) PluginPrincipal(_ context.Context, pluginID string, userID uint) (string, error) {
	if pluginID != "example.page" || userID != 41 {
		return "", errors.New("unexpected page principal")
	}
	return "p1.actor41", nil
}

func (pageActionTestHostServices) CallPluginHost(context.Context, string, string, string, json.RawMessage) (json.RawMessage, *pluginv1.HostCallError) {
	return nil, &pluginv1.HostCallError{Code: "forbidden"}
}

func TestPageActionUsesExactAuthenticatedSessionContext(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "page")
	cmd := exec.Command("go", "build", "-o", binary, "./testdata/page")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	payload, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	raw, keys := fixturePackage(t, func(manifest *Manifest, files map[string][]byte) {
		manifest.ID = "example.page"
		manifest.Capabilities = []string{PageCapability, ConfigCapability}
		manifest.Surfaces = []string{"account"}
		manifest.Components.UI = map[string]string{"account": "ui/account.html"}
		manifest.Components.Server = &struct {
			Executables map[string]string `json:"executables"`
		}{
			Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "runtimes/host/plugin"},
		}
		manifest.Contributions.Pages = []Page{{ID: "devices", Surface: "account", Title: "Devices", Purpose: "business"}}
		files["ui/account.html"] = []byte("<!doctype html><title>Devices</title>")
		files["runtimes/host/plugin"] = payload
	})
	manager, _, _ := testManager(t, keys)
	manager.SetHostServices(pageActionTestHostServices{})
	installation, err := importFixture(t, manager, raw)
	if err != nil {
		t.Fatal(err)
	}
	installation, err = manager.Action(context.Background(), installation.ID, "enable", "admin", installation.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.CreateSession(installation.ID, "devices", "account", 41, false, false)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.SessionPageAction(context.Background(), session.Token, 41, false, "devices.list", json.RawMessage(`{"cursor":"next"}`))
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(result, &response); err != nil {
		t.Fatal(err)
	}
	if response["actor_id"] != "p1.actor41" || response["page_id"] != "devices" || response["surface"] != "account" || response["action"] != "devices.list" {
		t.Fatalf("unexpected action context: %#v", response)
	}
	if _, err := manager.SessionPageAction(context.Background(), session.Token, 42, false, "devices.list", json.RawMessage(`{}`)); err == nil {
		t.Fatal("page action crossed the authenticated session identity")
	}
}
