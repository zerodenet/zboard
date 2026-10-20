package plugins

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"google.golang.org/grpc"
)

type commerceQuoteRPCStub struct {
	call func(context.Context, *pluginv1.CommerceQuoteRequest) (*pluginv1.CommerceQuoteResult, error)
}

func (s commerceQuoteRPCStub) EvaluateCommerceQuote(ctx context.Context, in *pluginv1.CommerceQuoteRequest, _ ...grpc.CallOption) (*pluginv1.CommerceQuoteResult, error) {
	return s.call(ctx, in)
}

func commerceQuoteFixture(t *testing.T) (Installation, commerce.DiscountQuoteContext, commerce.DiscountOffer) {
	t.Helper()
	v := Installation{PluginInstallation: model.PluginInstallation{ID: "example.quote", Enabled: true, State: "active", Generation: 3, ConfigRevision: 2, SigningKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), VersionID: strings.Repeat("b", 64), Publisher: "example"}, Admission: Admission{Accepted: true, Capabilities: []string{CommerceQuoteProviderCapability}}}
	v.Manifest.Capabilities = []string{CommerceQuoteProviderCapability}
	v.Manifest.Components.Server = &struct {
		Executables map[string]string `json:"executables"`
	}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "native"}}
	in := commerce.DiscountQuoteContext{Version: 1, BuyerID: 7, Operation: "new", PlanID: 1, PlanSKUID: 2, BasePriceMinor: 12000, Currency: "CNY", BillingUnit: "month", BillingValue: 1, InputFingerprint: strings.Repeat("c", 64)}
	terms := `{"discount_minor":2000}`
	hash, _ := commerce.DiscountTermsHash(terms)
	out := commerce.DiscountOffer{Status: "available", InputFingerprint: in.InputFingerprint, Currency: "CNY", DiscountMinor: 2000, ExpiresAt: time.Now().Add(time.Minute), Agreement: &commerce.DiscountAgreement{ID: "first", Version: "1", Scope: "initial_only", TermsHash: hash, TermsJSON: terms}}
	return v, in, out
}

func TestCommerceQuoteSignedManifestRequiresNativeWithoutPaymentGrant(t *testing.T) {
	if CommerceQuoteProviderCapability != pluginv1.CommerceQuoteProviderCapability {
		t.Fatal("native and SDK quote capability identifiers differ")
	}
	for _, native := range []bool{false, true} {
		raw, keys := fixturePackage(t, func(m *Manifest, files map[string][]byte) {
			m.Capabilities = append(m.Capabilities, CommerceQuoteProviderCapability)
			if native {
				m.Components.Server = &struct {
					Executables map[string]string `json:"executables"`
				}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "runtimes/native"}}
				files["runtimes/native"] = []byte("signed nonexecuted native fixture")
			}
		})
		pkg, err := ReadPackage(raw, keys)
		if !native {
			if err == nil {
				t.Fatal("quote provider without native runtime accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, grant := range pkg.Manifest.Capabilities {
			if grant == CommercePaymentsWriteCapability {
				t.Fatal("quote grant implied payment authority")
			}
		}
	}
	// The ordinary browser/page authority still cannot issue a payment fact.
	_, err := (SessionAuthority{UserID: 7}).Resolve(context.Background(), catalog.Credential{Kind: "plugin_session"}, "commerce.payments.record")
	if !errors.Is(err, catalog.ErrDenied) || OperationCapability("commerce.quote.evaluate") != "" {
		t.Fatal("quote provider acquired a page/payment command", err)
	}
}

func TestCommerceQuoteRuntimeAdmissionAndPostRPCFencesWithoutDatabase(t *testing.T) {
	for _, mutation := range []string{"disabled", "revoked", "unconfigured", "generation", "config-revision", "package", "signing-key"} {
		t.Run(mutation, func(t *testing.T) {
			before, in, offer := commerceQuoteFixture(t)
			after := before
			calls := 0
			client := commerceQuoteRPCStub{call: func(_ context.Context, request *pluginv1.CommerceQuoteRequest) (*pluginv1.CommerceQuoteResult, error) {
				calls++
				got, err := request.DiscountContext()
				if err != nil || got.BuyerID != in.BuyerID || got.BasePriceMinor != 12000 || request.Generation != before.Generation || request.ConfigRevision != before.ConfigRevision {
					t.Fatal("host context was not conveyed exactly", got, err)
				}
				switch mutation {
				case "disabled":
					after.Enabled = false
				case "revoked":
					after.Admission.Accepted = false
				case "unconfigured":
					after.ConfigRevision = 0
				case "generation":
					after.Generation++
				case "config-revision":
					after.ConfigRevision++
				case "package":
					after.VersionID = strings.Repeat("d", 64)
				case "signing-key":
					after.SigningKey = strings.Repeat("e", 64)
				}
				raw, _ := json.Marshal(offer)
				return &pluginv1.CommerceQuoteResult{OfferJson: raw}, nil
			}}
			if result, err := callCommerceQuoteProvider(context.Background(), before, client, in, func() error { return sameCommerceQuoteInstallation(before, after) }); err == nil || result.Provider.PluginID != "" || calls != 1 {
				t.Fatal("stale/revoked result escaped", result, err, calls)
			}
			// Revocation before invocation must not call the plugin at all.
			after.Enabled = false
			if _, err := callCommerceQuoteProvider(context.Background(), after, client, in, func() error { return nil }); err == nil || calls != 1 {
				t.Fatal("unavailable runtime invoked", calls, err)
			}
		})
	}
}

func TestCommerceQuoteNativeResponseValidationAndDeadline(t *testing.T) {
	before, in, _ := commerceQuoteFixture(t)
	for _, variant := range []string{"valid", "currency", "negative", "scope", "terms-hash", "unknown-field", "oversized"} {
		t.Run(variant, func(t *testing.T) {
			_, _, offer := commerceQuoteFixture(t)
			switch variant {
			case "currency":
				offer.Currency = "USD"
			case "negative":
				offer.DiscountMinor = -1
			case "scope":
				offer.Agreement.Scope = "all-users"
			case "terms-hash":
				offer.Agreement.TermsHash = strings.Repeat("0", 64)
			}
			raw, _ := json.Marshal(offer)
			if variant == "unknown-field" {
				raw = append(raw[:len(raw)-1], []byte(`,"payable_amount":1}`)...)
			}
			if variant == "oversized" {
				raw = []byte(strings.Repeat(" ", commerce.MaxDiscountQuoteBytes+1))
			}
			client := commerceQuoteRPCStub{call: func(context.Context, *pluginv1.CommerceQuoteRequest) (*pluginv1.CommerceQuoteResult, error) {
				return &pluginv1.CommerceQuoteResult{OfferJson: raw}, nil
			}}
			out, err := callCommerceQuoteProvider(context.Background(), before, client, in, func() error { return nil })
			if variant == "valid" {
				if err != nil || out.Provider.PluginID != before.ID || out.Provider.Generation != before.Generation || out.Offer.DiscountMinor != 2000 {
					t.Fatal(out, err)
				}
			} else if !errors.Is(err, commerce.ErrDiscountQuoteInvalid) {
				t.Fatal("invalid native output accepted", variant, out, err)
			}
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	client := commerceQuoteRPCStub{call: func(ctx context.Context, _ *pluginv1.CommerceQuoteRequest) (*pluginv1.CommerceQuoteResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	if out, err := callCommerceQuoteProvider(ctx, before, client, in, func() error { return nil }); !errors.Is(err, context.DeadlineExceeded) || out.Provider.PluginID != "" {
		t.Fatal("timed out quote returned", out, err)
	}
	manager := &Manager{capabilityWindows: map[string]capabilityAdmissionWindow{}}
	for i := 0; i < 60; i++ {
		if err := manager.admitPluginCall("commerce-quote:example.quote", 60); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.admitPluginCall("commerce-quote:example.quote", 60); !errors.Is(err, ErrRateLimited) {
		t.Fatal("unbounded provider calls", err)
	}
}
