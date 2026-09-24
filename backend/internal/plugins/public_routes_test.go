package plugins

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

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
