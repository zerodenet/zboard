package plugins

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func buildRoutePlugin(t *testing.T, version string) []byte {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "plugin")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	command := exec.Command("go", "build", "-ldflags", "-X main.pluginVersion="+version, "-o", binary, "./testdata/route")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build route plugin %s: %v %s", version, err, output)
	}
	payload, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func routePackage(t *testing.T, private ed25519.PrivateKey, public ed25519.PublicKey, version, routeID, routePath string) []byte {
	t.Helper()
	payload := buildRoutePlugin(t, version)
	return fixtureSignedPackage(t, private, public, func(manifest *Manifest, files map[string][]byte) {
		manifest.ID = "example.route"
		manifest.Version = version
		manifest.Capabilities = []string{ConfigCapability, HTTPRouteCapability}
		manifest.Surfaces = nil
		manifest.Contributions.Pages = nil
		manifest.Contributions.HTTPRoutes = []HTTPRoute{{ID: routeID, Method: "GET", Path: routePath}}
		manifest.Components.UI = nil
		manifest.Components.Server = &struct {
			Executables map[string]string `json:"executables"`
		}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "runtimes/host/plugin"}}
		files["runtimes/host/plugin"] = payload
	})
}

func TestSignedManifestPublicRouteDispatch(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "plugin")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	command := exec.Command("go", "build", "-o", binary, "./testdata/route")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build route plugin: %v %s", err, output)
	}
	payload, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	raw, keys := fixturePackage(t, func(manifest *Manifest, files map[string][]byte) {
		manifest.ID = "example.route"
		manifest.Capabilities = []string{ConfigCapability, HTTPRouteCapability}
		manifest.Surfaces = nil
		manifest.Contributions.Pages = nil
		manifest.Contributions.HTTPRoutes = []HTTPRoute{{ID: "discovery", Method: "GET", Path: "/.well-known/example/v1/discovery"}}
		manifest.Components.UI = nil
		manifest.Components.Server = &struct {
			Executables map[string]string `json:"executables"`
		}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "runtimes/host/plugin"}}
		files["runtimes/host/plugin"] = payload
	})
	manager, _, _ := testManager(t, keys)
	installation, err := importFixture(t, manager, raw)
	if err != nil {
		t.Fatal(err)
	}
	installation, err = manager.Action(context.Background(), installation.ID, "enable", "admin", installation.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	response, err := manager.HandlePublicRoute(context.Background(), "GET", "/.well-known/example/v1/discovery", "", nil)
	if err != nil || response.Status != 200 || string(response.Body) != `{"available":true}` {
		t.Fatalf("route response = %+v, %v", response, err)
	}
	if _, err := manager.HandlePublicRoute(context.Background(), "GET", "/.well-known/example/v1/other", "", nil); !errors.Is(err, ErrRouteNotFound) {
		t.Fatalf("undeclared route error = %v", err)
	}
	conflict := installation
	conflict.ID = "example.other-route"
	conflict.Manifest.ID = conflict.ID
	if err := manager.validatePublicRouteRegistrationLocked(conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate dynamic route registration error = %v", err)
	}
	if _, err := manager.Action(context.Background(), installation.ID, "disable", "admin", installation.Generation, false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.HandlePublicRoute(context.Background(), "GET", "/.well-known/example/v1/discovery", "", nil); !errors.Is(err, ErrRouteNotFound) {
		t.Fatalf("disabled route error = %v", err)
	}
}

func TestManifestRejectsUnsafeOrUnbackedPublicRoutes(t *testing.T) {
	for _, edit := range []func(*Manifest, map[string][]byte){
		func(manifest *Manifest, _ map[string][]byte) {
			manifest.Contributions.HTTPRoutes = []HTTPRoute{{ID: "route", Method: "POST", Path: "/api/v1/users"}}
		},
		func(manifest *Manifest, _ map[string][]byte) {
			manifest.Capabilities = append(manifest.Capabilities, HTTPRouteCapability)
			manifest.Contributions.HTTPRoutes = []HTTPRoute{{ID: "route", Method: "DELETE", Path: "/.well-known/example/v1/delete"}}
		},
	} {
		raw, keys := fixturePackage(t, edit)
		if _, err := ReadPackage(raw, keys); err == nil {
			t.Fatal("unsafe or unbacked public route was accepted")
		}
	}
}

func TestPublicRouteGenerationFollowsUpgradeAndUninstall(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]string{"test.publisher": base64.StdEncoding.EncodeToString(public)}
	manager, _, _ := testManager(t, keys)
	first, err := manager.Import(routePackage(t, private, public, "1.0.0", "discovery", "/.well-known/example/v1/discovery"), "admin")
	if err != nil {
		t.Fatal(err)
	}
	first, err = manager.Action(context.Background(), first.ID, "enable", "admin", first.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	oldProcess := manager.processes[first.ID]

	upgraded, err := manager.Import(routePackage(t, private, public, "1.1.0", "replacement", "/.well-known/example/v2/discovery"), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if oldProcess == nil || !oldProcess.client.Exited() || manager.processes[first.ID] == oldProcess {
		t.Fatal("upgrade did not revoke the previous route runtime generation")
	}
	if _, err := manager.HandlePublicRoute(context.Background(), "GET", "/.well-known/example/v1/discovery", "", nil); !errors.Is(err, ErrRouteNotFound) {
		t.Fatalf("upgraded plugin retained old route: %v", err)
	}
	if response, err := manager.HandlePublicRoute(context.Background(), "GET", "/.well-known/example/v2/discovery", "", nil); err != nil || response.Status != 200 {
		t.Fatalf("replacement route unavailable: %+v %v", response, err)
	}

	if _, err := manager.Action(context.Background(), upgraded.ID, "uninstall", "admin", upgraded.Generation, false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.HandlePublicRoute(context.Background(), "GET", "/.well-known/example/v2/discovery", "", nil); !errors.Is(err, ErrRouteNotFound) {
		t.Fatalf("uninstalled plugin retained replacement route: %v", err)
	}
}
