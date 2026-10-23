package plugins

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

// SQLite plus one signed native subprocess. Run only in a scheduled native
// fixture slot, separately from the database-free contract tests.
func TestCommerceQuoteRealNativeAdmissionAndReconfiguration(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "quote-plugin")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if raw, err := exec.Command("go", "build", "-p", "1", "-o", binary, "./testdata/quote").CombinedOutput(); err != nil {
		t.Fatalf("native build: %v %s", err, raw)
	}
	payload, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	raw, keys := fixturePackage(t, func(m *Manifest, files map[string][]byte) {
		m.ID = "example.quote"
		m.Capabilities = []string{ConfigCapability, CommerceQuoteProviderCapability}
		m.Surfaces, m.Contributions.Pages, m.Components.UI = nil, nil, nil
		m.Components.Server = &struct {
			Executables map[string]string `json:"executables"`
		}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "runtimes/native"}}
		files["runtimes/native"] = payload
	})
	m, db, _ := testManager(t, keys)
	ctx := context.Background()
	v, err := importFixture(t, m, raw)
	if err != nil {
		t.Fatal(err)
	}
	_, in, _ := commerceQuoteFixture(t)
	if _, err := m.EvaluateCommerceDiscount(ctx, v.ID, in); !errors.Is(err, ErrUnavailable) {
		t.Fatal("disabled provider invoked", err)
	}
	v, err = m.Action(ctx, v.ID, "enable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.EvaluateCommerceDiscount(ctx, v.ID, in); !errors.Is(err, ErrUnavailable) {
		t.Fatal("unconfigured provider invoked", err)
	}
	if _, err := m.SaveConfig(ctx, v.ID, "admin", 0, []byte(`{"enabled":true}`)); err != nil {
		t.Fatal(err)
	}
	out, err := m.EvaluateCommerceDiscount(ctx, v.ID, in)
	if err != nil || out.Provider.PluginID != v.ID || out.Provider.ConfigRevision != 1 || out.Offer.DiscountMinor != 2000 {
		t.Fatal(out, err)
	}
	in.Selection = "slow"
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	_, err = m.EvaluateCommerceDiscount(short, v.ID, in)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("native quote timeout accepted", err)
	}
	m.mu.Lock()
	api := m.processes[v.ID].api
	m.mu.Unlock()
	started := func() uint64 {
		t.Helper()
		status, err := api.Health(ctx, &pluginv1.Empty{})
		if err != nil {
			t.Fatal(err)
		}
		n, err := strconv.ParseUint(status.Message, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, mutation := range []string{"configuration", "generation", "authorization"} {
		previous := started()
		in.Selection = "slow"
		done := make(chan error, 1)
		go func() { _, err := m.EvaluateCommerceDiscount(ctx, v.ID, in); done <- err }()
		deadline := time.Now().Add(time.Second)
		for started() == previous {
			if !time.Now().Before(deadline) {
				t.Fatal("native call did not start")
			}
			time.Sleep(5 * time.Millisecond)
		}
		switch mutation {
		case "configuration":
			_, err = m.SaveConfig(ctx, v.ID, "admin", 1, []byte(`{"enabled":true,"revision":2}`))
		case "generation":
			err = db.Model(&model.PluginInstallation{}).Where("id = ?", v.ID).Update("generation", v.Generation+1).Error
		case "authorization":
			err = db.Where("plugin_id = ?", v.ID).Delete(&model.PluginAuthorization{}).Error
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := <-done; err == nil {
			t.Fatal("native result survived changed", mutation)
		}
	}
	beforeDenied := started()
	if _, err := m.EvaluateCommerceDiscount(ctx, v.ID, in); !errors.Is(err, ErrUnavailable) {
		t.Fatal("revoked capability invoked", err)
	}
	if started() != beforeDenied {
		t.Fatal("revoked quote reached native RPC")
	}
	// Restoring a test receipt is intentionally not used: production acceptance
	// remains lifecycle-owned, and this test finishes with revoked authority.
}
