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
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/experience"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"golang.org/x/crypto/bcrypt"
)

// Opt-in, isolated SQLite acceptance using the built application and real HTTP.
// Future now is passed only to the real expiry capability; no source purchase,
// payment, usage, approval or wallet fact is rewritten to manufacture eligibility.
func TestConsumptionRefundChromiumAcceptance(t *testing.T) {
	if os.Getenv("ZBOARD_CONSUMPTION_REFUND_BROWSER") != "1" {
		t.Skip("opt-in real Chromium consumption-refund acceptance")
	}
	evidence := os.Getenv("ZBOARD_CONSUMPTION_REFUND_BROWSER_EVIDENCE_DIR")
	web := os.Getenv("ZBOARD_CONSUMPTION_REFUND_BROWSER_WEB_DIR")
	script := os.Getenv("ZBOARD_CONSUMPTION_REFUND_BROWSER_SCRIPT")
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
	files, err := os.ReadDir(evidence)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		name := file.Name()
		if strings.HasSuffix(name, ".request") || strings.HasSuffix(name, ".done") || strings.HasSuffix(name, ".png") || strings.HasSuffix(name, "-private.html") || name == "fixture-private.json" || name == "browser-checks.json" || name == "financial-checks.json" || name == "backend-binding.json" || name == "protected-facts-private.json" || name == "browser-private.log" {
			t.Fatal("fresh evidence directory required", name)
		}
	}
	writeExclusive := func(name string, raw []byte) {
		t.Helper()
		file, err := os.OpenFile(filepath.Join(evidence, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.Write(raw)
		closeErr := file.Close()
		if writeErr != nil {
			t.Fatal(writeErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	privateJSON := func(name string, value any) {
		t.Helper()
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		writeExclusive(name, raw)
	}
	jsonHash := func(value any) string {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(raw)
		return hex.EncodeToString(hash[:])
	}

	f := newOrderFixture(t)
	if !datastore.IsSQLite(f.h.db) {
		t.Fatal("refund browser acceptance requires isolated SQLite")
	}
	// Configure a real eligible endpoint before purchase so fulfillment issues
	// credentials through its normal capability, not by inserting identities.
	attachOrderPublishEndpoint(t, f)
	// Real slot/navigation handlers require the same manager dependency as the
	// application. This isolated manager has no installed plugins or providers.
	manager, err := plugins.NewManager(f.h.db, f.h.credentialCipher, plugins.Options{Directory: t.TempDir()}, "v0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	f.h.SetPluginManager(manager)
	// Existing helper uses actual order/funding services and an authorized test
	// payment attempt + PaymentFact. It never marks a database order paid by hand.
	order := refundPaidOrder(t, f, 0)
	if order.Status != "paid" || order.PaidAmount != 1000 || order.FulfilledAt == nil || order.SubscriptionID == 0 || order.RefundAmount != 0 {
		t.Fatal("trusted source purchase did not fulfill")
	}
	walletMatches(t, f, 0, 0, 0)
	var originalSubscription model.Subscription
	if err := f.h.db.First(&originalSubscription, order.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	// Database row maps include persisted json:"-" fields too: a public model
	// JSON representation would silently omit epoch/token/credential evidence.
	rows := func(target any, orderBy, where string, args ...any) []map[string]any {
		t.Helper()
		out := []map[string]any{}
		if err := f.h.db.Model(target).Where(where, args...).Order(orderBy).Find(&out).Error; err != nil {
			t.Fatal("protected evidence read failed", err)
		}
		return out
	}
	paymentEvidence := func() any {
		t.Helper()
		return map[string]any{
			"funding":  rows(&model.OrderFunding{}, "order_id", "order_id = ?", order.ID),
			"attempts": rows(&model.PaymentAttempt{}, "id", "order_id = ?", order.ID),
			"receipts": rows(&model.PaymentReceipt{}, "id", "order_id = ?", order.ID),
			"events":   rows(&model.PaymentEvent{}, "id", "order_id = ?", order.ID),
		}
	}
	paymentHash := jsonHash(paymentEvidence())
	// Prove non-vacuous rights existed, including an access token obtained by
	// the real owned access capability rather than by inserting a token fixture.
	var createdCredentials []model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ?", order.SubscriptionID).Order("id").Find(&createdCredentials).Error; err != nil || len(createdCredentials) == 0 {
		t.Fatal("purchase created no credential to revoke", err)
	}
	access, err := f.h.services.SubscriptionAccess(f.h.credentialCipher).Read(t.Context(), 1, order.SubscriptionID)
	if err != nil || !access.Configured || access.Token == "" {
		t.Fatal("real owned token issuance failed", err)
	}
	var createdTokens []model.SubscriptionToken
	if err := f.h.db.Where("subscription_id = ?", order.SubscriptionID).Order("id").Find(&createdTokens).Error; err != nil || len(createdTokens) == 0 {
		t.Fatal("real access capability persisted no token", err)
	}
	for _, credential := range createdCredentials {
		if credential.Status != "active" || credential.Secret == "" || credential.RevokedAt != nil {
			t.Fatal("purchase credential was not usable before expiry")
		}
	}
	for _, token := range createdTokens {
		if token.TokenCiphertext == "" || token.RevokedAt != nil {
			t.Fatal("issued token was not usable before expiry")
		}
	}
	grace, err := entitlementstore.RecoveryGracePeriod(f.h.db)
	if err != nil {
		t.Fatal(err)
	}
	futureNow := originalSubscription.EndAt.Add(max(grace, 0) + time.Second)
	if !futureNow.After(time.Now().UTC()) {
		t.Fatal("fixture must demonstrate explicit future scheduler clock")
	}
	if _, err := f.h.services.CredentialExpiry(f.h.credentialCipher).ExpireDue(t.Context(), futureNow, 20); err != nil {
		t.Fatal("real future-clock expiry", err)
	}
	var closedOrder model.Order
	if err := f.h.db.First(&closedOrder, order.ID).Error; err != nil || closedOrder.SubscriptionEndedAt == nil || closedOrder.SubscriptionEndReason != "expired" || closedOrder.PurchaseSnapshot != order.PurchaseSnapshot || closedOrder.PaidAmount != order.PaidAmount || closedOrder.RefundAmount != 0 {
		t.Fatal("expiry did not preserve original paid source", err)
	}
	if jsonHash(paymentEvidence()) != paymentHash {
		t.Fatal("expiry changed payment evidence")
	}
	closedOrderRows := rows(&model.Order{}, "id", "id = ?", order.ID)
	if len(closedOrderRows) != 1 {
		t.Fatal("closed source order row missing")
	}
	closedOrderHash := jsonHash(closedOrderRows)
	walletMatches(t, f, 0, 0, 0)
	protectedEvidence := func() any {
		t.Helper()
		var sub model.Subscription
		if err := f.h.db.First(&sub, order.SubscriptionID).Error; err != nil || sub.Status != "ended" || sub.EndedAt == nil || sub.NextResetAt != nil {
			t.Fatal("subscription is not terminal after recovery window", err)
		}
		var credentials []model.ProtocolCredential
		var tokens []model.SubscriptionToken
		for _, target := range []any{&credentials, &tokens} {
			if err := f.h.db.Where("subscription_id = ?", sub.ID).Order("id").Find(target).Error; err != nil {
				t.Fatal(err)
			}
		}
		if len(credentials) != len(createdCredentials) || len(tokens) != len(createdTokens) {
			t.Fatal("terminal identity set was removed or replaced")
		}
		for i, credential := range credentials {
			if credential.ID != createdCredentials[i].ID || credential.Status != "revoked" || credential.Secret != "" || credential.RevokedAt == nil {
				t.Fatal("terminal credential remains usable")
			}
		}
		for i, token := range tokens {
			if token.ID != createdTokens[i].ID || token.RevokedAt == nil || token.TokenCiphertext != "" {
				t.Fatal("terminal token remains usable")
			}
		}
		return map[string]any{
			"subscription": rows(&model.Subscription{}, "id", "id = ?", sub.ID),
			"credentials":  rows(&model.ProtocolCredential{}, "id", "subscription_id = ?", sub.ID),
			"tokens":       rows(&model.SubscriptionToken{}, "id", "subscription_id = ?", sub.ID),
			"flows":        rows(&model.FlowUsage{}, "id", "subscription_id = ?", sub.ID),
			"traffic":      rows(&model.TrafficRecord{}, "id", "subscription_id = ?", sub.ID),
			"payment":      paymentEvidence(),
		}
	}
	protectedHash := jsonHash(protectedEvidence())
	privateJSON("protected-facts-private.json", protectedEvidence())

	// These are isolated test identities, not production credentials/settings.
	const password = "consumption-refund-browser-fixture-only"
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.User{}).Where("id = 1").Updates(map[string]any{"password": string(passwordHash), "is_admin": false}).Error; err != nil {
		t.Fatal(err)
	}
	admin := model.User{Email: "refund-browser-admin@example.test", Password: string(passwordHash), Status: userStatusActive, IsAdmin: true}
	other := model.User{Email: "refund-browser-other@example.test", Password: "unused", Status: userStatusActive}
	for _, user := range []*model.User{&admin, &other} {
		if err := f.h.db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	foreignTicket, err := f.h.services.Tickets.Create(t.Context(), other.ID, experience.NewTicket{TicketNo: "T-REFUND-BROWSER-FOREIGN", Subject: "Other owner's request", Category: "billing", Priority: 1, Body: "This is an unrelated owner's support record."}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.Installation{ID: 1, SiteName: "Consumption refund browser fixture", SiteURL: "http://127.0.0.1", InstalledAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	for route, fn := range map[string]http.HandlerFunc{
		"GET /api/v1/setup/status":                           f.h.SetupStatusHandler,
		"GET /api/v1/system/status":                          f.h.SystemStatusHandler,
		"GET /api/v1/system/configs":                         f.h.PublicSystemConfigsHandler,
		"POST /api/v1/auth/login":                            f.h.LoginHandler,
		"GET /api/v1/auth/me":                                f.h.MeHandler,
		"GET /api/v1/navigation":                             f.h.NavigationHandler,
		"GET /api/v1/account/identities/security":            f.h.ExternalPasswordStatusHandler,
		"GET /api/v1/plugin-ui/slots":                        f.h.PluginSlotCatalogHandler,
		"GET /api/v1/admin/system-info":                      f.h.AdminSystemInfoHandler,
		"GET /api/v1/orders":                                 f.h.OrderListHandler,
		"GET /api/v1/admin/orders":                           f.h.OrderListHandler,
		"GET /api/v1/admin/orders/{id}":                      f.h.AdminOrderGetHandler,
		"GET /api/v1/admin/orders/{id}/payment-events":       f.h.AdminOrderPaymentEventsHandler,
		"GET /api/v1/orders/{id}/payment-receipts":           f.h.PaymentReceiptsHandler,
		"GET /api/v1/admin/orders/{id}/payment-receipts":     f.h.PaymentReceiptsHandler,
		"POST /api/v1/admin/orders/{id}/consumption-refunds": f.h.AdminOrderConsumptionRefundsHandler,
		"GET /api/v1/admin/consumption-refunds/{id}":         f.h.AdminConsumptionRefundHandler,
		"GET /api/v1/account/wallet":                         f.h.WalletHandler,
		"GET /api/v1/admin/users/{id}/wallet":                f.h.AdminUserWalletHandler,
		"GET /api/v1/tickets":                                f.h.TicketListHandler,
		"POST /api/v1/tickets":                               f.h.TicketCreateHandler,
		"GET /api/v1/tickets/{id}":                           f.h.TicketGetHandler,
		"GET /api/v1/admin/tickets":                          f.h.TicketListHandler,
		"GET /api/v1/admin/tickets/{id}":                     f.h.TicketGetHandler,
		"GET /api/v1/admin/audit-logs":                       f.h.AuditLogsHandler,
		"GET /api/v1/admin/audit-logs/{id}":                  f.h.AuditLogDetailHandler,
		"GET /api/v1/admin/entity-references":                f.h.AdminEntityReferencesHandler,
		"GET /api/v1/subscriptions":                          f.h.SubscriptionsHandler,
		"GET /api/v1/subscription-templates":                 f.h.SubscriptionTemplateListHandler,
		"GET /api/v1/subscription/protocol-loads":            f.h.AccountProtocolLoadHandler,
		"GET /api/v1/account/subscriptions/{id}/access":      f.h.AccountSubscriptionAccessHandler,
	} {
		mux.HandleFunc(route, fn)
	}
	mux.HandleFunc("/api/", http.NotFound)
	assets := http.FileServer(http.Dir(web))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			assets.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(web, "index.html"))
	})
	var requestMu sync.Mutex
	var mutations []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Method != http.MethodHead {
			requestMu.Lock()
			mutations = append(mutations, r.Method+" "+r.URL.Path)
			requestMu.Unlock()
		}
		mux.ServeHTTP(w, r)
	}))
	defer server.Close()
	binaryPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	binaryHash := sha256.Sum256(binary)
	privateJSON("backend-binding.json", map[string]any{"test_binary_sha256": hex.EncodeToString(binaryHash[:]), "order_id": order.ID, "subscription_id": order.SubscriptionID, "payment_evidence_sha256": paymentHash, "closed_order_all_columns_sha256": closedOrderHash, "protected_facts_sha256": protectedHash, "created_credential_count": len(createdCredentials), "issued_token_count": len(createdTokens), "token_issuance": "real owned SubscriptionAccess.Read", "original_end_at": originalSubscription.EndAt, "effective_recovery_grace_seconds": int64(grace / time.Second), "expiry_now": futureNow, "clock_limit": "explicit synthetic future clock for expiry only; not natural billing-term passage"})
	privateJSON("fixture-private.json", map[string]any{"origin": server.URL, "owner_email": "reader@example.test", "admin_email": admin.Email, "password": password, "owner_id": 1, "admin_id": admin.ID, "order_id": order.ID, "subscription_id": order.SubscriptionID, "foreign_ticket_id": foreignTicket, "evidence_dir": evidence})
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	browserArgs := []string{script, "--fixture", filepath.Join(evidence, "fixture-private.json")}
	if chromium := os.Getenv("ZBOARD_BROWSER_CHROMIUM"); chromium != "" {
		browserArgs = append(browserArgs, "--chromium", chromium)
	}
	cmd := exec.CommandContext(ctx, "python3", browserArgs...)
	browserLog, err := os.OpenFile(filepath.Join(evidence, "browser-private.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer browserLog.Close()
	cmd.Stdout, cmd.Stderr = browserLog, browserLog
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	var browserErr error
	go func() { browserErr = cmd.Wait(); close(finished) }()
	defer func() {
		cancel()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-finished:
		case <-time.After(2 * time.Second):
			t.Error("browser child did not exit after cancellation")
		}
	}()
	wait := func(stage string) {
		t.Helper()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			if _, err := os.Stat(filepath.Join(evidence, stage+".request")); err == nil {
				return
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
			select {
			case <-finished:
				t.Fatalf("browser exited before %s: %v; see private log", stage, browserErr)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-ticker.C:
			}
		}
	}
	signal := func(stage string) {
		t.Helper()
		writeExclusive(stage+".done", []byte("ok"))
	}
	count := func(target any, where string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := f.h.db.Model(target).Where(where, args...).Count(&n).Error; err != nil {
			t.Fatal("financial evidence count failed", err)
		}
		return n
	}
	assertProtected := func() {
		t.Helper()
		if jsonHash(protectedEvidence()) != protectedHash {
			t.Fatal("refund/read/replay changed terminal rights, metering or payment evidence")
		}
	}
	assertUnapproved := func() {
		t.Helper()
		walletMatches(t, f, 0, 0, 0)
		if count(&model.ConsumptionRefund{}, "order_id = ?", order.ID) != 0 || count(&model.AuditLog{}, "action = ? AND target = ?", "order.consumption_refund", fmt.Sprintf("order:%d", order.ID)) != 0 {
			t.Fatal("rejected or read-only request created financial evidence")
		}
		var current model.Order
		if err := f.h.db.First(&current, order.ID).Error; err != nil || current.RefundAmount != 0 {
			t.Fatal("unapproved source order changed", err)
		}
		assertProtected()
	}
	wait("ticket")
	var ownerTicket model.Ticket
	if err := f.h.db.Where("user_id = ? AND subject = ?", 1, "核对已结束订阅的消费退款").First(&ownerTicket).Error; err != nil || ownerTicket.OrderID == nil || *ownerTicket.OrderID != order.ID || ownerTicket.SubscriptionID == nil || *ownerTicket.SubscriptionID != order.SubscriptionID || ownerTicket.WalletEntryID != nil {
		t.Fatal("browser ticket did not persist exact owned context", err)
	}
	if count(&model.Ticket{}, "user_id = ?", 1) != 1 || count(&model.TicketMessage{}, "ticket_id = ?", ownerTicket.ID) != 1 {
		t.Fatal("ticket creation automatically replayed")
	}
	assertUnapproved()
	signal("ticket_checked")
	wait("rejected")
	assertUnapproved()
	signal("rejection_checked")
	var approved model.ConsumptionRefund
	assertApproved := func() {
		t.Helper()
		var refund model.ConsumptionRefund
		if err := f.h.db.Where("order_id = ?", order.ID).First(&refund).Error; err != nil || refund.UserID != 1 || refund.ActorID != admin.ID || refund.TicketID != ownerTicket.ID || refund.AmountMinor != 300 || refund.Currency != "CNY" || refund.EntryID == 0 {
			t.Fatal("actual approved refund mismatch", err)
		}
		if approved.ID != 0 && (refund.ID != approved.ID || refund.EntryID != approved.EntryID) {
			t.Fatal("same-key replay changed approved identity")
		}
		approved = refund
		if count(&model.ConsumptionRefund{}, "order_id = ?", order.ID) != 1 || count(&model.WalletEntry{}, "kind = ? AND source_order_id = ?", "consumption_refund", order.ID) != 1 || count(&model.AuditLog{}, "action = ? AND target = ?", "order.consumption_refund", fmt.Sprintf("order:%d", order.ID)) != 1 {
			t.Fatal("refund/wallet/audit evidence missing or duplicated")
		}
		walletMatches(t, f, 300, 0, 1)
		var entry model.WalletEntry
		if err := f.h.db.First(&entry, refund.EntryID).Error; err != nil || entry.UserID != 1 || entry.BalanceDelta != 300 || entry.ReservedDelta != 0 || entry.SourceOrderID == nil || *entry.SourceOrderID != order.ID {
			t.Fatal("refund wallet source mismatch", err)
		}
		var budget model.OrderValueBudget
		if err := f.h.db.First(&budget, "order_id = ?", order.ID).Error; err != nil || budget.FundingMinor != 1000 || budget.RefundedMinor != 300 || budget.ConvertedMinor != 0 {
			t.Fatal("refund budget mismatch", err)
		}
		var current model.Order
		if err := f.h.db.First(&current, order.ID).Error; err != nil || current.Status != "paid" || current.PaidAmount != 1000 || current.RefundAmount != 300 || current.RefundedAt == nil {
			t.Fatal("original paid order altered", err)
		}
		currentRows := rows(&model.Order{}, "id", "id = ?", order.ID)
		if len(currentRows) != 1 {
			t.Fatal("source order row missing after approval")
		}
		for _, allowed := range []string{"refund_amount", "refunded_at", "updated_at"} {
			currentRows[0][allowed] = closedOrderRows[0][allowed]
		}
		if jsonHash(currentRows) != closedOrderHash {
			t.Fatal("approval rewrote fields beyond refund amount/time")
		}
		assertProtected()
	}
	wait("approved")
	assertApproved()
	signal("approval_checked")
	wait("verified")
	assertApproved()
	requestMu.Lock()
	requests := append([]string(nil), mutations...)
	requestMu.Unlock()
	approvalPath := "POST /api/v1/admin/orders/" + fmt.Sprint(order.ID) + "/consumption-refunds"
	counts := map[string]int{}
	for _, request := range requests {
		counts[request]++
	}
	if len(counts) != 3 || counts["POST /api/v1/auth/login"] != 2 || counts["POST /api/v1/tickets"] != 1 || counts[approvalPath] != 5 {
		t.Fatal("unexpected browser mutation/replay count", counts)
	}
	signal("verification_checked")
	select {
	case <-finished:
		if browserErr != nil {
			t.Fatal("browser acceptance failed; see private log", browserErr)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var browserResult struct {
		Passed bool `json:"passed"`
	}
	raw, err := os.ReadFile(filepath.Join(evidence, "browser-checks.json"))
	if err != nil || json.Unmarshal(raw, &browserResult) != nil || !browserResult.Passed {
		t.Fatal("browser did not record a successful completion", err)
	}
	privateJSON("financial-checks.json", map[string]any{"passed": true, "paid_minor_unchanged": 1000, "refund_minor": 300, "wallet_before_minor": 0, "wallet_after_minor": 300, "reserved_minor": 0, "refund_count": 1, "refund_wallet_entry_count": 1, "refund_audit_count": 1, "refund_id": approved.ID, "entry_id": approved.EntryID, "ticket_id": ownerTicket.ID, "owner_id": 1, "admin_id": admin.ID, "explicit_same_key_replays": 2, "rights_payment_metering_sha256_unchanged": protectedHash, "mutations": counts, "expiry_now": futureNow, "synthetic_clock_only_not_natural_term_passage": true})
	t.Log("real Chromium consumption-refund acceptance passed; private evidence", evidence)
}
