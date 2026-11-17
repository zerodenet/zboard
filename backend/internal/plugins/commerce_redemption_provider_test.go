package plugins

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

func TestCommerceRedemptionRequiresSignedNativeQuoteAdmissionAndNoPageMutationGrant(t *testing.T) {
	if CommerceDiscountRedemptionCapability != pluginv1.DiscountRedemptionCapability {
		t.Fatal("capability SDK ID differs")
	}
	if CommerceDiscountRedemptionCapability != commerce.DiscountRedemptionCapability {
		t.Fatal("capability core ID differs")
	}
	for _, variant := range []string{"no-native", "no-quote", "valid"} {
		t.Run(variant, func(t *testing.T) {
			raw, keys := fixturePackage(t, func(m *Manifest, files map[string][]byte) {
				m.Capabilities = append(m.Capabilities, CommerceDiscountRedemptionCapability)
				if variant != "no-quote" {
					m.Capabilities = append(m.Capabilities, CommerceQuoteProviderCapability)
				}
				if variant != "no-native" {
					m.Components.Server = &struct {
						Executables map[string]string `json:"executables"`
					}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "runtimes/native"}}
					files["runtimes/native"] = []byte("signed nonexecuted reservation fixture")
				}
			})
			pkg, err := ReadPackage(raw, keys)
			if variant != "valid" {
				if err == nil {
					t.Fatal("unsupported redemption admission accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, grant := range pkg.Manifest.Capabilities {
				if grant == CommercePaymentsWriteCapability {
					t.Fatal("reservation implied payment authority")
				}
			}
		})
	}
	if OperationCapability("commerce.discount.reserve") != "" || OperationCapability("commerce.discount.commit") != "" || OperationCapability("commerce.discount.release") != "" {
		t.Fatal("browser/page gained host-only redemption RPC")
	}
	_, err := (SessionAuthority{UserID: 7}).Resolve(context.Background(), catalog.Credential{Kind: "plugin_session"}, "commerce.payments.record")
	if !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("ordinary plugin page obtained payment authority: %v", err)
	}
}
