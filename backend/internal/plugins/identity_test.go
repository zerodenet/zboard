package plugins

import (
	"context"
	"gorm.io/gorm"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

func TestIdentityCapabilityRequiresServerAndValidMetadata(t *testing.T) {
	raw, keys := fixturePackage(t, func(m *Manifest, _ map[string][]byte) { m.Capabilities = append(m.Capabilities, IdentityCapability) })
	if _, err := ReadPackage(raw, keys); err == nil {
		t.Fatal("UI-only identity provider accepted")
	}
	for _, p := range []*pluginv1.IdentityProvider{nil, {Issuer: "https://id.example.test", AuthorizationEndpoint: "javascript:alert(1)", ClientId: "a", Scopes: []string{"openid"}}, {Issuer: "https://id.example.test", AuthorizationEndpoint: "https://id.example.test/auth", ClientId: "a", Scopes: []string{"openid", "unsafe scope"}}} {
		if validIdentityProvider(p) {
			t.Fatal("unsafe metadata accepted")
		}
	}
}
func TestRealIdentityPluginRevokesUncommittedCoreWork(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "plugin")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", binary, "./testdata/identity").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	payload, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	raw, keys := fixturePackage(t, func(m *Manifest, files map[string][]byte) {
		m.ID = "example.identity"
		m.Capabilities = []string{"zboard.config.v1", IdentityCapability}
		m.Surfaces = nil
		m.Contributions.Pages = nil
		m.Components.UI = nil
		m.Components.Server = &struct {
			Executables map[string]string `json:"executables"`
		}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "runtimes/host/plugin"}}
		files["runtimes/host/plugin"] = payload
	})
	m, _, _ := testManager(t, keys)
	ctx := context.Background()
	v, err := importApproved(t, m, raw)
	if err != nil {
		t.Fatal(err)
	}
	v, err = m.Action(ctx, v.ID, "enable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.SaveConfig(ctx, v.ID, "admin", 0, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	providers, err := m.IdentityProviders()
	if err != nil || len(providers) != 2 {
		t.Fatal("identity catalog missing", err)
	}
	selected, err := m.IdentityProvider(ctx, v.ID+"~second")
	if err != nil || selected.ID != v.ID || selected.Provider.ProviderId != "second" || selected.IdentityKey() != v.ID+"~second" {
		t.Fatal("provider selection failed", err)
	}
	if err := m.ExchangeIdentity(ctx, selected, &pluginv1.IdentityExchange{Code: "valid", Issuer: selected.Provider.Issuer}, func(*pluginv1.VerifiedIdentity, *gorm.DB) error { t.Fatal("wrong provider reached core"); return nil }); err == nil {
		t.Fatal("provider mismatch accepted")
	}
	snapshot, err := m.IdentityProvider(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := &pluginv1.IdentityExchange{Code: "valid", Issuer: snapshot.Provider.Issuer}
	commits := 0
	commit := func(*pluginv1.VerifiedIdentity, *gorm.DB) error { commits++; return nil }
	if err := m.ExchangeIdentity(ctx, snapshot, request, commit); err != nil || commits != 1 {
		t.Fatal("identity exchange failed", err)
	}
	request.Code = "invalid"
	if m.ExchangeIdentity(ctx, snapshot, request, commit) == nil || commits != 1 {
		t.Fatal("invalid identity reached core")
	}
	request.Code = "valid"
	if _, err = m.SaveConfig(ctx, v.ID, "admin", 1, []byte(`{"changed":true}`)); err != nil {
		t.Fatal(err)
	}
	if m.ExchangeIdentity(ctx, snapshot, request, commit) == nil || commits != 1 {
		t.Fatal("stale configuration reached core")
	}
	snapshot, err = m.IdentityProvider(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Action(ctx, v.ID, "disable", "admin", v.Generation, false, ""); err != nil {
		t.Fatal(err)
	}
	if m.ExchangeIdentity(ctx, snapshot, request, commit) == nil || commits != 1 {
		t.Fatal("disabled plugin reached core")
	}
	if m.WithIdentityProvider(snapshot, func(*gorm.DB) error { commits++; return nil }) == nil || commits != 1 {
		t.Fatal("disabled plugin completed session")
	}
	// Revoking identity permission must fence already verified work even when the configurable service can still run.
	current, err := m.load(v.ID)
	if err != nil {
		t.Fatal(err)
	}
	current, err = m.Authorize(v.ID, "admin", current.Digest, current.Generation, []string{ConfigCapability}, true)
	if err != nil {
		t.Fatal(err)
	}
	current, err = m.Action(ctx, current.ID, "enable", "admin", current.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if providers, err := m.IdentityProviders(); err != nil || len(providers) != 0 {
		t.Fatal("revoked identity capability advertised", err)
	}
	if m.WithIdentityProvider(snapshot, func(*gorm.DB) error { commits++; return nil }) == nil || commits != 1 {
		t.Fatal("revoked permission completed core work")
	}

}
