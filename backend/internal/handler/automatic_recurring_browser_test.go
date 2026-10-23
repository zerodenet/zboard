package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"golang.org/x/crypto/bcrypt"
)

// Opt in with ZBOARD_RECURRING_BROWSER=1 and absolute WEB_DIR, SCRIPT and
// EVIDENCE_DIR paths bearing the same prefix. Uses isolated SQLite, a genuinely
// signed native fixture, real wallet payment facts, and the built application.
// Coordination files request test phases; they never carry API responses.
func TestAutomaticRecurringChromiumAcceptance(t *testing.T) {
	if os.Getenv("ZBOARD_RECURRING_BROWSER") != "1" {
		t.Skip("opt-in real Chromium recurring acceptance")
	}
	evidence, web, script := os.Getenv("ZBOARD_RECURRING_BROWSER_EVIDENCE_DIR"), os.Getenv("ZBOARD_RECURRING_BROWSER_WEB_DIR"), os.Getenv("ZBOARD_RECURRING_BROWSER_SCRIPT")
	for _, p := range []string{evidence, web, script} {
		if !filepath.IsAbs(p) {
			t.Fatal("browser paths must be absolute")
		}
	}
	if err := os.MkdirAll(evidence, 0700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(evidence)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("evidence must be a private 0700 directory, not a symlink", err)
	}
	entries, err := os.ReadDir(evidence)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".request") || strings.HasSuffix(name, ".done") || name == "fixture-private.json" || name == "browser-checks.json" || name == "financial-checks.json" {
			t.Fatal("fresh evidence directory required; existing fixture or phase file", name)
		}
	}
	f := newOrderFixture(t)
	manager, provider, generation, signer := recurringNativeSetup(t, f)
	const password = "recurring-browser-fixture-only"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.User{}).Where("id = 1").Update("password", string(hash)).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.Installation{ID: 1, SiteName: "Recurring browser fixture", SiteURL: "http://127.0.0.1", InstalledAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	f.token, _, err = f.h.issueToken(authClaims{UserID: 1, Email: "reader@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	request := commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, IdempotencyKey: "browser-native-recurring-root", Discount: &commerce.OrderDiscountSelection{ProviderID: provider, Selection: "recurring"}}
	request.QuoteFingerprint = recurringPreview(t, f, request).QuoteFingerprint
	root, policy := recurringWalletPay(t, f, recurringCreate(t, f, request).ID)
	if policy.Discount.Quote.Provider.SigningKeyFingerprint != signer {
		t.Fatal("root signed identity mismatch")
	}
	walletMatches(t, f, 40000, 0, 3)
	mux := http.NewServeMux()
	for path, fn := range map[string]http.HandlerFunc{
		"/api/v1/setup/status": f.h.SetupStatusHandler, "/api/v1/system/status": f.h.SystemStatusHandler, "/api/v1/system/configs": f.h.PublicSystemConfigsHandler,
		"/api/v1/auth/login": f.h.LoginHandler, "/api/v1/auth/me": f.h.MeHandler, "/api/v1/navigation": f.h.NavigationHandler,
		"/api/v1/account/identities/security": f.h.ExternalPasswordStatusHandler, "/api/v1/subscriptions": f.h.SubscriptionsHandler,
		"/api/v1/subscription-templates": f.h.SubscriptionTemplateListHandler, "/api/v1/subscription/protocol-loads": f.h.AccountProtocolLoadHandler,
		"/api/v1/plugin-ui/slots": f.h.PluginSlotCatalogHandler,
	} {
		mux.HandleFunc(path, fn)
	}
	mux.HandleFunc("/api/v1/subscriptions/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/automatic-renewal") {
			f.h.AutomaticRenewalHandler(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/v1/plans/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/skus") {
			f.h.PublicPlanSKUListCommerceHandler(w, r)
			return
		}
		f.h.PublicPlanDetailCommerceHandler(w, r)
	})
	mux.HandleFunc("/api/v1/account/subscriptions/", f.h.AccountSubscriptionAccessHandler)
	// Unregistered APIs fail visibly rather than returning invented fixture data.
	mux.HandleFunc("/api/", http.NotFound)
	assets := http.FileServer(http.Dir(web))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			assets.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(web, "index.html"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	privateJSON := func(name string, value any) {
		t.Helper()
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(evidence, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	binaryHash := sha256.Sum256(binary)
	privateJSON("fixture-private.json", map[string]any{"origin": server.URL, "email": "reader@example.test", "password": password, "subscription_id": root.SubscriptionID, "plan_id": f.planRecord.ID, "evidence_dir": evidence})
	privateJSON("backend-binding.json", map[string]any{"test_binary_sha256": hex.EncodeToString(binaryHash[:]), "root_order_id": root.ID, "subscription_id": root.SubscriptionID, "provider_signer_sha256": signer, "limits": []string{"isolated SQLite and loopback HTTP only", "selected real handlers, not full server router acceptance", "only end_at and next_attempt_at compressed; no natural-term passage claim", "no direct approval/payment/wallet mutation"}})
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", script, "--fixture", filepath.Join(evidence, "fixture-private.json"))
	browserLog, err := os.OpenFile(filepath.Join(evidence, "browser-private.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer browserLog.Close()
	cmd.Stdout = browserLog
	cmd.Stderr = browserLog
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		cancel()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()
	wait := func(stage string) {
		t.Helper()
		timer := time.NewTicker(100 * time.Millisecond)
		defer timer.Stop()
		for {
			if _, err := os.Stat(filepath.Join(evidence, stage+".request")); err == nil {
				return
			}
			select {
			case err := <-done:
				t.Fatalf("browser exited before %s: %v; see private log", stage, err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-timer.C:
			}
		}
	}
	signal := func(stage string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(evidence, stage+".done"), []byte("ok"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	makeDue := func() {
		t.Helper()
		due := time.Now().UTC().Add(-time.Second)
		if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", root.SubscriptionID).Update("end_at", due).Error; err != nil {
			t.Fatal(err)
		}
		if err := f.h.db.Model(&model.SubscriptionAutomaticRenewal{}).Where("subscription_id = ?", root.SubscriptionID).Update("next_attempt_at", due).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := f.h.services.AutomaticRenewals(f.h.credentialCipher, f.h.zeroMieruAccess)
	wait("enabled")
	makeDue()
	if n, err := service.Process(ctx, 20); err != nil || n != 1 {
		t.Fatal("real automatic debit", n, err)
	}
	if n, err := service.Process(ctx, 20); err != nil || n != 0 {
		t.Fatal("automatic replay", n, err)
	}
	var approved model.SubscriptionAutomaticRenewal
	if err := f.h.db.First(&approved, "subscription_id = ?", root.SubscriptionID).Error; err != nil || !approved.Enabled || approved.LastOrderID == nil {
		t.Fatal("missing approved renewal", err)
	}
	var paid model.Order
	if err := f.h.db.First(&paid, *approved.LastOrderID).Error; err != nil || paid.Status != "paid" || paid.AmountCents != 12000 || paid.DiscountAmount != 2000 || paid.PaidAmount != 10000 || paid.PayableAmount != 10000 {
		t.Fatal("real debit money", err)
	}
	snap, err := commerce.DecodePurchaseSnapshot(paid.PurchaseSnapshot)
	if err != nil || commerce.ValidatePurchaseDiscount(commerce.Order(paid), snap) != nil || snap.Discount.Quote.Provider.SigningKeyFingerprint != signer {
		t.Fatal("paid signed discount snapshot", err)
	}
	walletMatches(t, f, 30000, 0, 5)
	signal("renewed")
	wait("reenabled")
	if err := f.h.db.First(&approved, "subscription_id = ?", root.SubscriptionID).Error; err != nil || !approved.Enabled {
		t.Fatal("missing explicit reapproval", err)
	}
	if _, err := manager.Action(ctx, provider, "disable", "admin", generation, false, ""); err != nil {
		t.Fatal(err)
	}
	var orders, fundings int64
	if err := f.h.db.Model(&model.Order{}).Count(&orders).Error; err != nil {
		t.Fatal("read order count before provider outage", err)
	}
	if err := f.h.db.Model(&model.OrderFunding{}).Count(&fundings).Error; err != nil {
		t.Fatal("read funding count before provider outage", err)
	}
	makeDue()
	if n, err := service.Process(ctx, 20); err != nil || n != 1 {
		t.Fatal("provider unavailable attempt", n, err)
	}
	var paused model.SubscriptionAutomaticRenewal
	if err := f.h.db.First(&paused, "subscription_id = ?", root.SubscriptionID).Error; err != nil || !paused.Enabled || paused.State != "agreement_unavailable" || paused.Revision != approved.Revision || paused.ApprovalSnapshot != approved.ApprovalSnapshot || paused.AgreementDigest != approved.AgreementDigest {
		t.Fatal("provider outage changed consent", err)
	}
	var afterOrders, afterFundings int64
	if err := f.h.db.Model(&model.Order{}).Count(&afterOrders).Error; err != nil {
		t.Fatal("read order count after provider outage", err)
	}
	if err := f.h.db.Model(&model.OrderFunding{}).Count(&afterFundings).Error; err != nil {
		t.Fatal("read funding count after provider outage", err)
	}
	if afterOrders != orders || afterFundings != fundings {
		t.Fatal("outage created order/funding")
	}
	walletMatches(t, f, 30000, 0, 5)
	signal("unavailable")
	wait("withdrawn")
	var revoked model.SubscriptionAutomaticRenewal
	if err := f.h.db.First(&revoked, "subscription_id = ?", root.SubscriptionID).Error; err != nil || revoked.Enabled || revoked.State != "disabled" || revoked.Revision != paused.Revision+1 {
		t.Fatal("explicit withdrawal failed", err)
	}
	if n, err := service.Process(ctx, 20); err != nil || n != 0 {
		t.Fatal("withdrawn consent remained due", n, err)
	}
	walletMatches(t, f, 30000, 0, 5)
	signal("withdrawn")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("browser acceptance failed", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	privateJSON("financial-checks.json", map[string]any{"passed": true, "base_minor": 12000, "discount_minor": 2000, "debit_minor": 10000, "currency": "CNY", "wallet_before_auto_minor": 40000, "wallet_after_auto_minor": 30000, "wallet_after_unavailable_and_withdrawal_minor": 30000, "outage_orders_unchanged": true, "outage_fundings_unchanged": true, "approval_preserved_during_outage": true, "replay_debits": 0, "withdrawn_worker_actions": 0, "date_compression": "subscription.end_at and automatic.next_attempt_at only"})
	t.Log(fmt.Sprintf("real Chromium recurring acceptance passed; private evidence %s", evidence))
}
