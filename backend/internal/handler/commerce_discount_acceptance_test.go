package handler

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
)

func discountNativePackage(t *testing.T, public ed25519.PublicKey, private ed25519.PrivateKey) []byte {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "quote-plugin")
	if raw, err := exec.Command("go", "build", "-p", "1", "-o", binary, "../plugins/testdata/quote").CombinedOutput(); err != nil {
		t.Fatalf("build signed quote fixture: %v %s", err, raw)
	}
	payload, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	path := "runtimes/native"
	manifest := plugins.Manifest{SchemaVersion: 1, ID: "example.quote", Name: "首单优惠", Description: "Signed isolated quote fixture", Version: "1.0.0", Requires: plugins.Requirements{ZBoard: ">=0.0.1", Tested: []string{"0.0.1"}, Protocol: 1, Bridge: 1}, Capabilities: []string{plugins.ConfigCapability, plugins.CommerceQuoteProviderCapability}}
	manifest.Components.Server = &struct {
		Executables map[string]string `json:"executables"`
	}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: path}}
	digest := sha256.Sum256(payload)
	manifest.Files = map[string]string{path: hex.EncodeToString(digest[:])}
	raw, _ := json.Marshal(manifest)
	signature, _ := json.Marshal(plugins.Signature{Algorithm: "ed25519", KeyID: "quote.tests", PublicKey: base64.StdEncoding.EncodeToString(public), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, raw))})
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, data := range map[string][]byte{path: payload, "manifest.json": raw, "signature.json": signature} {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// True local SQLite, signed native RPC, HTTP quote/create and ordinary core
// payment/fulfillment/renewal. This is neither an external coupon integration
// nor proof that a remote kernel has received its persisted publication intent.
func TestCommerceDiscountNativeHTTPWalletAndAutomaticRenewal(t *testing.T) {
	assertCommerceDiscountNativeHTTPWalletAndAutomaticRenewal(t, newOrderFixture(t))
}

func TestMySQLCommerceDiscountNativeHTTPWalletAndAutomaticRenewal(t *testing.T) {
	assertCommerceDiscountNativeHTTPWalletAndAutomaticRenewal(t, newMySQLBusinessOrderFixture(t))
}

func assertCommerceDiscountNativeHTTPWalletAndAutomaticRenewal(t *testing.T, f orderFixture) {
	t.Helper()
	priceForFunding(t, f, 12000)
	if err := f.h.db.Model(&f.planRecord).Update("is_renewable", true).Error; err != nil {
		t.Fatal(err)
	}
	attachOrderPublishEndpoint(t, f)
	fundTestWallet(t, f, 22000)
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	f.token, _, _ = f.h.issueToken(authClaims{UserID: 1, Email: "reader@example.test"})
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	manager, err := plugins.NewManager(f.h.db, f.h.credentialCipher, plugins.Options{Directory: t.TempDir(), TrustedPublishers: map[string]string{"quote.tests": base64.StdEncoding.EncodeToString(public)}}, "v0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	f.h.SetPluginManager(manager)
	installation, err := manager.Import(discountNativePackage(t, public, private), "admin")
	if err != nil {
		t.Fatal(err)
	}
	list := func(token string) (*httptest.ResponseRecorder, []commerce.DiscountProviderChoice) {
		response := httptest.NewRecorder()
		f.h.CommerceDiscountProvidersHandler(response, announcementRequest(http.MethodGet, "/api/v1/commerce/discount-providers", token, ""))
		var result struct {
			Data []commerce.DiscountProviderChoice
		}
		_ = json.Unmarshal(response.Body.Bytes(), &result)
		return response, result.Data
	}
	if response, _ := list(""); response.Code != http.StatusUnauthorized {
		t.Fatal("provider discovery became anonymous", response.Code)
	}
	if response, choices := list(f.token); response.Code != 200 || len(choices) != 0 {
		t.Fatal("disabled provider advertised", response.Code, choices)
	}
	installation, err = manager.Action(t.Context(), installation.ID, "enable", "admin", installation.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if response, choices := list(f.token); response.Code != 200 || len(choices) != 0 {
		t.Fatal("unconfigured provider advertised", response.Code, choices)
	}
	_, err = manager.SaveConfig(t.Context(), installation.ID, "admin", 0, []byte(`{"enabled":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if response, choices := list(f.token); response.Code != 200 || len(choices) != 1 || choices[0].Name != "首单优惠" || choices[0].ProviderID != installation.ID || choices[0].AgreementScope != "quote_required" || strings.Contains(response.Body.String(), "signing_key") || strings.Contains(response.Body.String(), "config_revision") {
		t.Fatal("public discovery lost scope or leaked installation internals", response.Code, choices)
	}
	request := commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, IdempotencyKey: "native-first-discount", Discount: &commerce.OrderDiscountSelection{ProviderID: installation.ID}}
	call := func(handler http.HandlerFunc, path string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(request)
		response := httptest.NewRecorder()
		handler(response, announcementRequest(http.MethodPost, path, f.token, string(raw)))
		return response
	}
	previewResponse := call(f.h.OrderPreviewHandler, "/api/v1/orders/preview")
	var preview struct{ Data commerce.OrderPreview }
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &preview); err != nil || previewResponse.Code != 200 || preview.Data.BasePriceMinor != 12000 || preview.Data.DiscountAmount != 2000 || preview.Data.PayableAmount != 10000 || preview.Data.QuoteFingerprint == "" {
		t.Fatal("native HTTP quote", previewResponse.Code, previewResponse.Body.String(), err)
	}
	if response := call(f.h.OrderCreateCommerceValidatedHandler, "/api/v1/orders"); response.Code != 400 || !strings.Contains(response.Body.String(), "validation_failed") {
		t.Fatal("unconfirmed discount created an order", response.Code, response.Body.String())
	}
	request.QuoteFingerprint = preview.Data.QuoteFingerprint
	response := call(f.h.OrderCreateCommerceValidatedHandler, "/api/v1/orders")
	var created struct{ Data commerce.Order }
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil || response.Code != 200 || created.Data.ID == 0 {
		t.Fatal("confirmed discount create", response.Code, response.Body.String(), err)
	}
	if _, err := fundingService(f).Configure(t.Context(), 1, created.Data.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal("explicit balance allocation", err)
	}
	if _, err := fundingService(f).Pay(t.Context(), 1, created.Data.ID); err != nil {
		t.Fatal("ordinary wallet fulfillment", err)
	}
	var paid model.Order
	if err := f.h.db.First(&paid, created.Data.ID).Error; err != nil || paid.PaidAmount != 10000 || paid.AmountCents != 12000 || paid.DiscountAmount != 2000 || paid.Status != "paid" || paid.SubscriptionID == 0 || paid.EntitlementValueSnapshot == "" {
		t.Fatal("discount changed payment facts or locked base", err)
	}
	if _, err := manager.Action(t.Context(), installation.ID, "disable", "admin", installation.Generation, false, ""); err != nil {
		t.Fatal(err)
	}
	if response, choices := list(f.token); response.Code != 200 || len(choices) != 0 {
		t.Fatal("disabled provider retained", response.Code, choices)
	}
	if response := call(f.h.OrderCreateCommerceValidatedHandler, "/api/v1/orders"); response.Code != 200 {
		t.Fatal("lost-response recovery depended on stopped provider", response.Code, response.Body.String())
	}
	request.IdempotencyKey = "new-request-stopped-provider"
	if response := call(f.h.OrderCreateCommerceValidatedHandler, "/api/v1/orders"); response.Code != 503 || !strings.Contains(response.Body.String(), "discount_unavailable") {
		t.Fatal("unavailable offer became a charge or unexplained error", response.Code, response.Body.String())
	}
	priceForFunding(t, f, 15000)
	manual, err := f.h.services.OrderCreation.Preview(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, TargetSubscriptionID: paid.SubscriptionID})
	if err != nil || manual.AmountCents != 12000 || manual.PayableAmount != 12000 {
		t.Fatal("first discount or new catalog price changed renewal base", manual, err)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	at := sub.EndAt.Add(time.Minute)
	auto := commerce.AutomaticRenewals{Repository: commercestore.AutomaticRenewals{DB: f.h.db, Issuer: zeroadapter.Issuer{Cipher: f.h.credentialCipher, Mieru: f.h.zeroMieruAccess}, Now: func() time.Time { return at }}}
	view, err := auto.Read(t.Context(), 1, sub.ID, f.skuRecord.ID)
	if err != nil || view.Offer == nil || view.Offer.BasePriceMinor != 12000 {
		t.Fatal("verified initial-only history blocked agreed base", view, err)
	}
	if _, err := auto.Set(t.Context(), 1, sub.ID, commerce.AutomaticRenewalRequest{Enabled: true, PlanSKUID: f.skuRecord.ID, QuoteFingerprint: view.Offer.QuoteFingerprint, ExpectedRevision: view.Revision}); err != nil {
		t.Fatal(err)
	}
	if n, err := auto.Process(t.Context(), 20); err != nil || n != 1 {
		t.Fatal("wallet-only renewal after stopped initial-only provider", n, err)
	}
	if n, err := auto.Process(t.Context(), 20); err != nil || n != 0 {
		t.Fatal("automatic renewal duplicated", n, err)
	}
	view, err = auto.Read(t.Context(), 1, sub.ID, 0)
	if err != nil || view.LastOrderID == nil {
		t.Fatal("missing durable renewal result", view, err)
	}
	var renewal model.Order
	if err := f.h.db.First(&renewal, *view.LastOrderID).Error; err != nil || renewal.PaidAmount != 12000 || renewal.DiscountAmount != 0 || renewal.Channel != "wallet" || renewal.Status != "paid" {
		t.Fatal("renewal charged an incorrect amount", err)
	}
	wallet, err := f.h.services.Wallet.Read(t.Context(), 1, "CNY", 0, 100)
	if err != nil || wallet.BalanceMinor != 0 || wallet.ReservedMinor != 0 {
		t.Fatal("discount became spendable money or duplicate payment", wallet, err)
	}
}
