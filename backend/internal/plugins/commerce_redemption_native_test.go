package plugins

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"gorm.io/gorm"
)

type nativeRedemptionObservation struct {
	Calls          uint64          `json:"calls"`
	Generation     uint64          `json:"generation"`
	ConfigRevision uint64          `json:"config_revision"`
	Request        json.RawMessage `json:"request"`
}

func nativeRedemptionStatus(t *testing.T, api pluginv1.PluginControlClient) nativeRedemptionObservation {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	health, err := api.Health(ctx, &pluginv1.Empty{})
	var out nativeRedemptionObservation
	if err != nil || !health.GetHealthy() || json.Unmarshal([]byte(health.GetMessage()), &out) != nil {
		t.Fatal("cannot observe real native RPC", health, err)
	}
	return out
}

func nativeRedemptionAPI(t *testing.T, m *Manager, id string) pluginv1.PluginControlClient {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.processes[id]
	if p == nil || p.client == nil || p.client.Exited() {
		t.Fatal("signed redemption process is not alive")
	}
	if _, ok := p.api.(pluginv1.CommerceRedemptionProviderClient); !ok {
		t.Fatal("real RuntimeAPI omitted redemption client")
	}
	return p.api
}

func nativeRedemptionHashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func nativeRedemptionRequest(t *testing.T, provider commerce.DiscountProviderIdentity, key, agreement string) commerce.DiscountRedemptionRequest {
	t.Helper()
	termsHash, err := commerce.DiscountTermsHash(`{"discount_minor":2000,"kind":"native-host-fixture"}`)
	if err != nil {
		t.Fatal(err)
	}
	b := commerce.DiscountRedemptionBinding{Version: 1, Key: key, OrderID: 4, BuyerID: 7, InputFingerprint: strings.Repeat("c", 64), Provider: provider, AgreementID: agreement, AgreementVersion: "1", TermsHash: termsHash, BaseMinor: 12000, DiscountMinor: 2000, PayableMinor: 10000, Currency: "CNY"}
	r := commerce.DiscountRedemptionRequest{Version: 1, Action: "query", Binding: b}
	return rehashNativeRedemption(t, r)
}

func rehashNativeRedemption(t *testing.T, r commerce.DiscountRedemptionRequest) commerce.DiscountRedemptionRequest {
	t.Helper()
	var err error
	r.BindingHash, err = commerce.DiscountRedemptionHash(r.Binding)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func importNativeRedemption(t *testing.T, payload []byte) (*Manager, *gorm.DB, Installation) {
	t.Helper()
	raw, keys := fixturePackage(t, func(m *Manifest, files map[string][]byte) {
		m.ID = "example.redemption"
		m.Capabilities = []string{ConfigCapability, CommerceQuoteProviderCapability, CommerceDiscountRedemptionCapability}
		m.Surfaces, m.Contributions.Pages, m.Components.UI = nil, nil, nil
		m.Components.Server = &struct {
			Executables map[string]string `json:"executables"`
		}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "runtimes/native"}}
		files["runtimes/native"] = payload
	})
	m, db, _ := testManager(t, keys)
	v, err := importFixture(t, m, raw)
	if err != nil || !v.Admission.Accepted || !hasCapability(v, CommerceDiscountRedemptionCapability) {
		t.Fatal("signed native redemption admission failed", v, err)
	}
	return m, db, v
}

func enableNativeRedemption(t *testing.T, m *Manager, v Installation) (Installation, commerce.DiscountProviderIdentity) {
	t.Helper()
	var err error
	v, err = m.Action(t.Context(), v.ID, "enable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveConfig(t.Context(), v.ID, "admin", 0, []byte(`{"enabled":true}`)); err != nil {
		t.Fatal(err)
	}
	_, quoteContext, _ := commerceQuoteFixture(t)
	quote, err := m.EvaluateCommerceDiscount(t.Context(), v.ID, quoteContext)
	if err != nil || quote.Offer.Redemption != commerce.DiscountRedemptionGuarantee || quote.Provider.PluginID != v.ID {
		t.Fatal("real signed quote did not bind redemption runtime", quote, err)
	}
	m.mu.Lock()
	v, err = m.load(v.ID)
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return v, quote.Provider
}

// One compiled fixture is shared by this test's independent admitted hosts.
// Calls use the real signed package, native subprocess and RuntimeAPI/gRPC path.
// Its state is deliberately in memory: this is no external persistence test.
func TestCommerceRedemptionRealNativeHostAdmissionRuntimeAndFences(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "redemption-plugin")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if raw, err := exec.Command("go", "build", "-p", "1", "-o", binary, "./testdata/redemption").CombinedOutput(); err != nil {
		t.Fatalf("native redemption build: %v %s", err, raw)
	}
	payload, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("admission-wire-validation-and-recovery", func(t *testing.T) { assertNativeRedemptionWireAndRecovery(t, payload) })
	for _, mutation := range []string{"configuration", "generation", "version", "signer", "runtime", "disabled", "revoked", "redemption-withdrawn"} {
		t.Run("post-rpc-"+mutation, func(t *testing.T) { assertNativeRedemptionPostFence(t, payload, mutation) })
	}
}

func assertNativeRedemptionWireAndRecovery(t *testing.T, payload []byte) {
	m, _, v := importNativeRedemption(t, payload)
	// A structurally valid historic request must fail before native execution
	// while disabled or while the admitted native process lacks configuration.
	provider := commerce.DiscountProviderIdentity{PluginID: v.ID, SigningKeyFingerprint: keyFingerprint(v.SigningKey), PackageDigest: v.VersionID, Generation: max(1, v.Generation), ConfigRevision: 1}
	r := nativeRedemptionRequest(t, provider, strings.Repeat("a", 64), "finite")
	if out, err := m.RedeemDiscount(t.Context(), r); !errors.Is(err, ErrUnavailable) || out.Key != "" {
		t.Fatal("disabled provider redemption reached a usable result", out, err)
	}
	var err error
	v, err = m.Action(t.Context(), v.ID, "enable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	api := nativeRedemptionAPI(t, m, v.ID)
	if out, err := m.RedeemDiscount(t.Context(), r); !errors.Is(err, ErrUnavailable) || out.Key != "" || nativeRedemptionStatus(t, api).Calls != 0 {
		t.Fatal("unconfigured provider invoked redemption", out, err)
	}
	if _, err := m.SaveConfig(t.Context(), v.ID, "admin", 0, []byte(`{"enabled":true}`)); err != nil {
		t.Fatal(err)
	}
	_, quoteContext, _ := commerceQuoteFixture(t)
	quote, err := m.EvaluateCommerceDiscount(t.Context(), v.ID, quoteContext)
	if err != nil || quote.Offer.Redemption != commerce.DiscountRedemptionGuarantee {
		t.Fatal("native quote transport did not advertise admitted redemption", quote, err)
	}
	r = nativeRedemptionRequest(t, quote.Provider, strings.Repeat("a", 64), "finite")
	call := func(request commerce.DiscountRedemptionRequest, state string) commerce.DiscountRedemptionResult {
		t.Helper()
		out, err := m.RedeemDiscount(t.Context(), request)
		seen := nativeRedemptionStatus(t, nativeRedemptionAPI(t, m, v.ID))
		raw, _ := json.Marshal(request)
		if err != nil || out.State != state || out.Key != request.Binding.Key || out.BindingHash != request.BindingHash || !bytes.Equal(seen.Request, raw) {
			t.Fatal("native redemption binding/action was not conveyed exactly", out, seen, err)
		}
		return out
	}
	call(r, "absent")
	r.Action = "reserve"
	reserved := call(r, "reserved")
	r.Token = reserved.Token
	r.Action = "query"
	call(r, "reserved")
	r.Action = "commit"
	call(r, "committed")
	r.Action = "release"
	call(r, "committed")
	second := nativeRedemptionRequest(t, quote.Provider, strings.Repeat("b", 64), "finite")
	second.Action = "reserve"
	second.Token = call(second, "reserved").Token
	second.Action = "release"
	call(second, "released")

	for _, variant := range []string{"wrong-hash", "wrong-key", "wrong-token", "long-token", "long-message", "wrong-state", "wrong-guarantee", "wrong-version", "oversized", "empty", "unknown-field", "trailing-json"} {
		bad := nativeRedemptionRequest(t, quote.Provider, strings.Repeat("e", 64), variant)
		// Each fault selects an independent immutable binding at the fixture.
		// The key changes too, so a previous binding cannot short-circuit the
		// intended host result-validation boundary with a remote conflict.
		bad.Binding.Key = nativeRedemptionHashText(variant)
		bad = rehashNativeRedemption(t, bad)
		bad.Action, bad.Token = "commit", "original-exclusive-token"
		want := commerce.ErrDiscountRedemptionConflict
		if variant == "wrong-guarantee" {
			want = commerce.ErrDiscountRedemptionUnsupported
		}
		if out, err := m.RedeemDiscount(t.Context(), bad); !errors.Is(err, want) || out.Key != "" || out.Token != "" {
			t.Fatal("malicious native redemption reply escaped host validation", variant, out, err)
		}
	}
	for _, item := range []struct {
		variant string
		want    error
	}{{"unimplemented", commerce.ErrDiscountRedemptionUnsupported}, {"transport-deadline", context.DeadlineExceeded}, {"transport-canceled", context.Canceled}} {
		bad := nativeRedemptionRequest(t, quote.Provider, nativeRedemptionHashText(item.variant), item.variant)
		if out, err := m.RedeemDiscount(t.Context(), bad); !errors.Is(err, item.want) || out.Key != "" {
			t.Fatal("real redemption gRPC terminal result was reclassified", item.variant, out, err)
		}
	}
	beforeTimeout := nativeRedemptionStatus(t, api).Calls
	short, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	waiting := nativeRedemptionRequest(t, quote.Provider, nativeRedemptionHashText("timeout"), "wait-timeout")
	out, err := m.RedeemDiscount(short, waiting)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) || out.Key != "" || nativeRedemptionStatus(t, api).Calls != beforeTimeout+1 {
		t.Fatal("native timeout failed to exercise exactly one real RPC or returned usable evidence", out, err)
	}
	releaseNativeRedemption(t, api)
	beforeRejected := nativeRedemptionStatus(t, api).Calls
	badHash := r
	badHash.BindingHash = strings.Repeat("0", 64)
	badToken := r
	badToken.Token = strings.Repeat("x", 513)
	for _, invalid := range []commerce.DiscountRedemptionRequest{badHash, badToken} {
		if out, err := m.RedeemDiscount(t.Context(), invalid); !errors.Is(err, commerce.ErrDiscountRedemptionConflict) || out.Key != "" {
			t.Fatal("invalid request reached a usable native result", out, err)
		}
	}
	if nativeRedemptionStatus(t, api).Calls != beforeRejected {
		t.Fatal("invalid request reached native RPC")
	}

	// Legitimate disable/enable starts another actual process and advances the
	// generation. Committed reconfiguration also advances config revision.
	m.mu.Lock()
	v, err = m.load(v.ID)
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	v, err = m.Action(t.Context(), v.ID, "disable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	v, err = m.Action(t.Context(), v.ID, "enable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveConfig(t.Context(), v.ID, "admin", 1, []byte(`{"enabled":true,"revision":2}`)); err != nil {
		t.Fatal(err)
	}
	api = nativeRedemptionAPI(t, m, v.ID)
	// Deterministic protocol replies from the new in-memory fixture prove only
	// authorization to recover old bindings. They do not prove stock survived.
	r.Action = "query"
	call(r, "absent")
	r.Action = "commit"
	call(r, "committed")
	call(second, "released")
	seen := nativeRedemptionStatus(t, api)
	if seen.Generation == quote.Provider.Generation || seen.ConfigRevision == quote.Provider.ConfigRevision {
		t.Fatal("test did not exercise a new native generation/configuration")
	}
	var carried commerce.DiscountRedemptionRequest
	if json.Unmarshal(seen.Request, &carried) != nil || carried.Binding.Provider != quote.Provider {
		t.Fatal("recovery rewrote the immutable purchased runtime binding", carried)
	}
	for _, variant := range []string{"original-runtime", "generation", "config", "package", "signer"} {
		old := r
		old.Action, old.Token = "reserve", ""
		if variant != "original-runtime" {
			_, currentContext, _ := commerceQuoteFixture(t)
			fresh, err := m.EvaluateCommerceDiscount(t.Context(), v.ID, currentContext)
			if err != nil {
				t.Fatal(err)
			}
			old.Binding.Provider = fresh.Provider
			switch variant {
			case "generation":
				old.Binding.Provider.Generation++
			case "config":
				old.Binding.Provider.ConfigRevision++
			case "package":
				old.Binding.Provider.PackageDigest = strings.Repeat("0", 64)
			case "signer":
				old.Binding.Provider.SigningKeyFingerprint = strings.Repeat("0", 64)
			}
			old = rehashNativeRedemption(t, old)
		}
		before := nativeRedemptionStatus(t, api).Calls
		if out, err := m.RedeemDiscount(t.Context(), old); !errors.Is(err, ErrConflict) || out.Key != "" || nativeRedemptionStatus(t, api).Calls != before {
			t.Fatal("new reservation did not require exact current runtime/signer", variant, out, err)
		}
		if variant == "signer" {
			old.Action = "query"
			if out, err := m.RedeemDiscount(t.Context(), old); !errors.Is(err, ErrConflict) || out.Key != "" || nativeRedemptionStatus(t, api).Calls != before {
				t.Fatal("different signer recovered an old reservation", out, err)
			}
		}
	}
	_, currentContext, _ := commerceQuoteFixture(t)
	fresh, err := m.EvaluateCommerceDiscount(t.Context(), v.ID, currentContext)
	if err != nil {
		t.Fatal(err)
	}
	newReservation := nativeRedemptionRequest(t, fresh.Provider, nativeRedemptionHashText("fresh-runtime"), "finite")
	newReservation.Action = "reserve"
	call(newReservation, "reserved")
}

func releaseNativeRedemption(t *testing.T, api pluginv1.PluginControlClient) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := api.HandlePageAction(ctx, &pluginv1.PageActionRequest{Action: "fixture.release"}); err != nil {
		t.Fatal("native fixture barrier could not be released", err)
	}
}

func assertNativeRedemptionPostFence(t *testing.T, payload []byte, mutation string) {
	m, db, v := importNativeRedemption(t, payload)
	v, provider := enableNativeRedemption(t, m, v)
	api := nativeRedemptionAPI(t, m, v.ID)
	r := nativeRedemptionRequest(t, provider, nativeRedemptionHashText(mutation), "wait-"+mutation)
	switch mutation {
	case "generation", "disabled":
		r.Action = "reserve"
	case "version", "revoked":
		r.Action, r.Token = "commit", "fixture-existing-hold"
	case "signer", "redemption-withdrawn":
		r.Action, r.Token = "release", "fixture-existing-hold"
	}
	before := nativeRedemptionStatus(t, api).Calls
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	type outcome struct {
		result commerce.DiscountRedemptionResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() { out, err := m.RedeemDiscount(ctx, r); done <- outcome{result: out, err: err} }()
	joined := false
	defer func() {
		cancel()
		if !joined {
			<-done
		}
	}()
	deadline := time.Now().Add(time.Second)
	for nativeRedemptionStatus(t, api).Calls == before {
		if !time.Now().Before(deadline) {
			t.Fatal("actual native redemption did not reach the barrier")
		}
		time.Sleep(5 * time.Millisecond)
	}
	var err error
	want := ErrConflict
	switch mutation {
	case "configuration":
		_, err = m.SaveConfig(ctx, v.ID, "admin", 1, []byte(`{"revision":2}`))
	case "generation":
		err = db.Model(&model.PluginInstallation{}).Where("id = ?", v.ID).Update("generation", v.Generation+1).Error
	case "signer":
		err = db.Model(&model.PluginInstallation{}).Where("id = ?", v.ID).Update("signing_key", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))).Error
	case "version":
		// Controlled host-authority DB fault injection: expose a newly committed
		// admitted version while the prior native response remains in flight.
		err = db.Transaction(func(tx *gorm.DB) error {
			var version model.PluginVersion
			if err := tx.First(&version, "id = ?", v.VersionID).Error; err != nil {
				return err
			}
			version.ID, version.Digest, version.Version = nativeRedemptionHashText("new-version"), nativeRedemptionHashText("new-version"), "1.0.1"
			manifest := v.Manifest
			manifest.Version = version.Version
			encoded, _ := json.Marshal(manifest)
			version.Manifest = string(encoded)
			if err := tx.Create(&version).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.PluginAuthorization{}).Where("plugin_id = ?", v.ID).Update("digest", version.Digest).Error; err != nil {
				return err
			}
			return tx.Model(&model.PluginInstallation{}).Where("id = ?", v.ID).Update("version_id", version.ID).Error
		})
	case "runtime":
		m.mu.Lock()
		old := m.processes[v.ID]
		var replacement *process
		replacement, err = m.prepare(ctx, v)
		if err == nil {
			m.processes[v.ID] = replacement
			t.Cleanup(old.close)
		}
		m.mu.Unlock()
	case "disabled":
		want = ErrUnavailable
		err = db.Model(&model.PluginInstallation{}).Where("id = ?", v.ID).Update("enabled", false).Error
	case "revoked":
		want = ErrUnavailable
		err = db.Where("plugin_id = ?", v.ID).Delete(&model.PluginAuthorization{}).Error
	case "redemption-withdrawn":
		err = db.Transaction(func(tx *gorm.DB) error {
			manifest := v.Manifest
			manifest.Capabilities = []string{ConfigCapability, CommerceQuoteProviderCapability}
			raw, _ := json.Marshal(manifest)
			caps, _ := json.Marshal(manifest.Capabilities)
			if err := tx.Model(&model.PluginVersion{}).Where("id = ?", v.VersionID).Update("manifest", string(raw)).Error; err != nil {
				return err
			}
			return tx.Model(&model.PluginAuthorization{}).Where("plugin_id = ?", v.ID).Update("capabilities", string(caps)).Error
		})
	}
	if err != nil {
		t.Fatal("native post-RPC fence mutation failed", mutation, err)
	}
	releaseNativeRedemption(t, api)
	got := <-done
	joined = true
	if !errors.Is(got.err, want) || got.result.Key != "" || got.result.Token != "" {
		t.Fatal("stale native reservation evidence escaped host fence", mutation, got.result, got.err)
	}
	if mutation == "disabled" || mutation == "revoked" || mutation == "redemption-withdrawn" {
		before := nativeRedemptionStatus(t, api).Calls
		r.Binding.AgreementID = "finite"
		r = rehashNativeRedemption(t, r)
		expected := ErrUnavailable
		if mutation == "redemption-withdrawn" {
			expected = commerce.ErrDiscountRedemptionUnsupported
		}
		if out, err := m.RedeemDiscount(t.Context(), r); !errors.Is(err, expected) || out.Key != "" || nativeRedemptionStatus(t, api).Calls != before {
			t.Fatal("withdrawn host authority still reached native RPC", mutation, out, err)
		}
	}
}
