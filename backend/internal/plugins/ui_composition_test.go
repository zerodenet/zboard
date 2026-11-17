package plugins

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	hcplugin "github.com/hashicorp/go-plugin"
	"github.com/zerodenet/zboard/backend/internal/model"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"google.golang.org/grpc"
)

type compositionIdentityClient struct {
	pluginv1.PluginControlClient
	calls atomic.Int32
	fail  atomic.Bool
	wait  <-chan struct{}
}

func (c *compositionIdentityClient) ListIdentityProviders(ctx context.Context, _ *pluginv1.Empty, _ ...grpc.CallOption) (*pluginv1.IdentityProviderList, error) {
	c.calls.Add(1)
	if c.wait != nil {
		select {
		case <-c.wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if c.fail.Load() {
		return nil, errors.New("offline")
	}
	return &pluginv1.IdentityProviderList{Providers: []*pluginv1.IdentityProviderOption{{Id: "github", Name: "GitHub"}, {Id: "work", Name: "企业账号"}}}, nil
}
func compositionFixture(t *testing.T) (*Manager, Installation) {
	raw, keys := fixturePackage(t, func(m *Manifest, files map[string][]byte) {
		m.Capabilities = append(m.Capabilities, IdentityCapability, UISlotCapability)
		m.Surfaces = []string{"public", "account", "admin"}
		m.Components.Server = &struct {
			Executables map[string]string `json:"executables"`
		}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "runtimes/host/plugin"}}
		files["runtimes/host/plugin"] = []byte("signed fixture, never executed")
		m.Contributions.Slots = []Slot{{ID: "login", Surface: "public", Slot: "auth.login.methods", Title: "企业登录", Entrypoint: "ui/index.html"}, {ID: "overview", Surface: "account", Slot: "account.overview.cards", Title: "概览信息", Entrypoint: "ui/index.html"}, {ID: "actions", Surface: "admin", Slot: "admin.user.actions", Title: "用户操作", Entrypoint: "ui/index.html"}}
	})
	m, db, _ := testManager(t, keys)
	v, err := importFixture(t, m, raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.PluginInstallation{}).Where("id=?", v.ID).Updates(map[string]any{"enabled": true, "state": "active", "config_revision": 1}).Error; err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	v, err = m.load(v.ID)
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return m, v
}
func TestUICompositionRetainsUnavailableDeclarationsAndScopesSurfaces(t *testing.T) {
	m, v := compositionFixture(t)
	out, err := m.UIComposition(t.Context(), "public", 0, false)
	if err != nil || len(out.Slots) != 1 || out.Slots[0].Availability != "unavailable" || out.Slots[0].Presentation != "identity" {
		t.Fatal(out, err)
	}
	if _, err := m.UIComposition(t.Context(), "account", 0, false); err == nil {
		t.Fatal("anonymous private snapshot")
	}
	if _, err := m.UIComposition(t.Context(), "admin", 1, false); err == nil {
		t.Fatal("member admin snapshot")
	}
	private, err := m.UIComposition(t.Context(), "account", 1, false)
	if err != nil || len(private.Slots) != 1 || private.Slots[0].ReservedHeight != 224 {
		t.Fatal(private, err)
	}
	m.db.Model(&model.PluginInstallation{}).Where("id=?", v.ID).Update("state", "error")
	out, err = m.UIComposition(t.Context(), "account", 1, false)
	if err != nil || len(out.Slots) != 1 || out.Slots[0].Availability != "unavailable" {
		t.Fatal(out, err)
	}
	m.db.Model(&model.PluginInstallation{}).Where("id=?", v.ID).Update("enabled", false)
	out, err = m.UIComposition(t.Context(), "public", 0, false)
	if err != nil || len(out.Slots) != 0 {
		t.Fatal(out, err)
	}
}
func TestUICompositionCachesOnlyMetadataAndKeepsNamesAcrossRuntimeFailure(t *testing.T) {
	m, v := compositionFixture(t)
	client := &compositionIdentityClient{}
	m.processes[v.ID] = &process{api: client, client: hcplugin.NewClient(&hcplugin.ClientConfig{})}
	defer delete(m.processes, v.ID)
	first, err := m.UIComposition(t.Context(), "public", 0, false)
	if err != nil || len(first.Slots[0].Providers) != 2 || first.Slots[0].Providers[0].Name != "GitHub" {
		t.Fatal(first, err)
	}
	second, err := m.UIComposition(t.Context(), "public", 0, false)
	if err != nil || second.Revision != first.Revision || client.calls.Load() != 1 {
		t.Fatal(second, err, client.calls.Load())
	}
	m.mu.Lock()
	entry := m.identityPresentation[v.ID]
	entry.expires = time.Time{}
	m.identityPresentation[v.ID] = entry
	m.mu.Unlock()
	client.fail.Store(true)
	offline, err := m.UIComposition(t.Context(), "public", 0, false)
	if err != nil || offline.Slots[0].Availability != "unavailable" || len(offline.Slots[0].Providers) != 2 {
		t.Fatal(offline, err)
	}
	if offline.Revision == first.Revision {
		t.Fatal("availability change lost revision")
	}
	// Revision changes cannot borrow another configuration's names or authority.
	m.db.Model(&model.PluginInstallation{}).Where("id=?", v.ID).Update("config_revision", 2)
	changed, err := m.UIComposition(t.Context(), "public", 0, false)
	if err != nil || len(changed.Slots[0].Providers) != 0 {
		t.Fatal(changed, err)
	}
	raw := strings.Join([]string{first.Slots[0].Providers[0].ID, first.Slots[0].Providers[1].ID}, ",")
	if !strings.Contains(raw, v.ID+"~github") {
		t.Fatal(raw)
	}
}
func TestUICompositionCoalescesConcurrentMetadataReads(t *testing.T) {
	m, v := compositionFixture(t)
	gate := make(chan struct{})
	client := &compositionIdentityClient{wait: gate}
	m.processes[v.ID] = &process{api: client, client: hcplugin.NewClient(&hcplugin.ClientConfig{})}
	defer delete(m.processes, v.ID)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := m.UIComposition(t.Context(), "public", 0, false); results <- err }()
	}
	deadline := time.After(3 * time.Second)
	for client.calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("metadata call never started")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	close(gate)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if client.calls.Load() != 1 {
		t.Fatal("metadata stampede", client.calls.Load())
	}
}
