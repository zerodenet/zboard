package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

func createTopUp(t testing.TB, f orderFixture, amount int64, key string) model.Order {
	t.Helper()
	out := httptest.NewRecorder()
	f.h.WalletTopUpHandler(out, announcementRequest("POST", "/api/v1/account/wallet/topups", f.token, fmt.Sprintf(`{"amount_minor":%d,"currency":"CNY","idempotency_key":%q}`, amount, key)))
	if out.Code != 200 {
		t.Fatalf("create topup: %d %s", out.Code, out.Body.String())
	}
	var response struct{ Data model.Order }
	if err := json.Unmarshal(out.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}
func TestWalletTopUpVerifiedAccountingAndRecovery(t *testing.T) {
	assertWalletTopUp(t, newOrderFixture(t), false)
}
func TestMySQLWalletTopUpVerifiedAccountingAndRecovery(t *testing.T) {
	assertWalletTopUp(t, newMySQLOrderFixture(t), true)
}
func assertWalletTopUp(t *testing.T, f orderFixture, mysql bool) {
	t.Helper()
	for _, body := range []string{
		`{"amount_minor":12000,"currency":"CNY","idempotency_key":"forged-topup-status","status":"paid"}`,
		`{"amount_minor":12000,"currency":"CNY","idempotency_key":"forged-topup-owner","user_id":2}`,
	} {
		out := httptest.NewRecorder()
		f.h.WalletTopUpHandler(out, announcementRequest("POST", "/api/v1/account/wallet/topups", f.token, body))
		if out.Code != http.StatusBadRequest {
			t.Fatalf("unsupported topup authority fields accepted: %d %s", out.Code, out.Body.String())
		}
	}
	order := createTopUp(t, f, 12000, "topup-intent-0001")
	if again := createTopUp(t, f, 12000, "topup-intent-0001"); again.ID != order.ID {
		t.Fatal("lost creation response duplicated topup")
	}
	if order.PlanID != 0 || order.PlanSKUID != 0 || order.SubscriptionID != 0 || order.OrderType != "topup" {
		t.Fatal("topup invented product or entitlement")
	}
	if out := f.pay(t, order.ID, false); out.Code != http.StatusBadRequest {
		t.Fatalf("unverified administrator transition created cash: %d", out.Code)
	}
	issued, err := f.h.services.Integrations().Issue(t.Context(), 1, identity.IntegrationIssue{Name: "trusted receipt", Scopes: []string{"commerce.payments.record"}, ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	fact := commerce.PaymentFact{OrderID: order.ID, ProviderEventID: "topup-receipt-1", ProviderTradeNo: "topup-provider-trade-1", Status: "paid", AmountMinor: 12000, Currency: "CNY", OccurredAt: time.Now().UTC()}
	invoke := func() *httptest.ResponseRecorder {
		raw, _ := json.Marshal(fact)
		req := httptest.NewRequest("POST", "/api/v1/integrations/capabilities/commerce.payments.record/invoke", strings.NewReader(string(raw)))
		req.Header.Set("Authorization", "Bearer "+issued.Token)
		out := httptest.NewRecorder()
		f.h.IntegrationInvokeHandler(out, pathvar.WithVars(req, map[string]string{"name": "commerce.payments.record"}))
		return out
	}
	if mysql {
		if err := f.h.db.Exec(`CREATE TRIGGER reject_wallet_entry BEFORE INSERT ON wallet_entries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'ledger unavailable'`).Error; err != nil {
			t.Fatal(err)
		}
	} else {
		if err := f.h.db.Exec(`CREATE TRIGGER reject_wallet_entry BEFORE INSERT ON wallet_entries BEGIN SELECT RAISE(ABORT, 'ledger unavailable'); END`).Error; err != nil {
			t.Fatal(err)
		}
	}
	if out := invoke(); out.Code != 500 {
		t.Fatalf("ledger failure acknowledged: %d %s", out.Code, out.Body.String())
	}
	for _, table := range []string{"payment_events", "wallet_accounts", "wallet_entries"} {
		var count int64
		if err := f.h.db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("partial %s: %d %v", table, count, err)
		}
	}
	var actual model.Order
	if err := f.h.db.First(&actual, order.ID).Error; err != nil || actual.Status != "pending" {
		t.Fatalf("partial topup: %+v %v", actual, err)
	}
	if err := f.h.db.Exec("DROP TRIGGER reject_wallet_entry").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if i == 2 {
			fact.ProviderEventID = "topup-receipt-new-event-same-trade"
		}
		if out := invoke(); out.Code != 200 {
			t.Fatalf("receipt replay: %d %s", out.Code, out.Body.String())
		}
	}
	wallet, err := f.h.services.Wallet.Read(t.Context(), 1, "CNY", 0, 50)
	if err != nil || wallet.BalanceMinor != 12000 || wallet.ReservedMinor != 0 || wallet.AvailableMinor != 12000 || len(wallet.Entries) != 1 {
		t.Fatalf("wallet receipt: %+v %v", wallet, err)
	}
	for _, table := range []string{"subscriptions", "protocol_credentials", "quota_events", "node_config_publishes"} {
		var count int64
		if err := f.h.db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("topup granted %s: %d %v", table, count, err)
		}
	}
	other, err := f.h.services.Wallet.Read(t.Context(), 1, "USD", 0, 50)
	if err != nil || other.BalanceMinor != 0 || len(other.Entries) != 0 {
		t.Fatalf("currency balances combined: %+v %v", other, err)
	}
	read := httptest.NewRecorder()
	f.h.WalletHandler(read, announcementRequest("GET", "/api/v1/account/wallet?currency=CNY", f.token, ""))
	if read.Code != 200 || read.Header().Get("Cache-Control") != "no-store" || !strings.Contains(read.Body.String(), `"balance_minor":"12000"`) {
		t.Fatalf("wallet HTTP: %d %s", read.Code, read.Body.String())
	}
	if err := f.h.db.Exec("DELETE FROM orders WHERE id = ?", order.ID).Error; err == nil {
		t.Fatal("source order deletion erased financial evidence")
	}
	if err := f.h.db.Exec("DELETE FROM users WHERE id = ?", 1).Error; err == nil {
		t.Fatal("owner deletion erased financial evidence")
	}
	// Canceling an unpaid top-up cannot make a later verified receipt disappear.
	late := createTopUp(t, f, 500, "topup-intent-late")
	if _, err := f.h.services.OrderCancellation.Owned(t.Context(), 1, late.ID); err != nil {
		t.Fatal(err)
	}
	fact.OrderID, fact.ProviderEventID, fact.ProviderTradeNo, fact.AmountMinor = late.ID, "late-paid", "late-provider-trade", 500
	if out := invoke(); out.Code != 200 {
		t.Fatalf("late paid topup lost funds: %d %s", out.Code, out.Body.String())
	}
	wallet, err = f.h.services.Wallet.Read(t.Context(), 1, "CNY", 0, 50)
	if err != nil || wallet.BalanceMinor != 12500 || len(wallet.Entries) != 2 {
		t.Fatalf("late receipt balance: %+v %v", wallet, err)
	}
	for i := 0; i < 2; i++ {
		if err := datastore.RunMigrations(f.h.db); err != nil {
			t.Fatal(err)
		}
	}
	retained, err := f.h.services.Wallet.Read(t.Context(), 1, "CNY", 0, 50)
	if err != nil || retained.BalanceMinor != wallet.BalanceMinor || len(retained.Entries) != 2 {
		t.Fatalf("repeat migration lost financial facts: %+v %v", retained, err)
	}
}

func TestWalletSchemaRefusesLostFinancialEvidence(t *testing.T) {
	assertWalletLostSchema(t, newOrderFixture(t))
}
func TestMySQLWalletSchemaRefusesLostFinancialEvidence(t *testing.T) {
	assertWalletLostSchema(t, newMySQLOrderFixture(t))
}
func assertWalletLostSchema(t *testing.T, f orderFixture) {
	t.Helper()
	// Inject physical ledger loss in this isolated test database. MySQL
	// otherwise correctly prevents dropping the referenced financial table.
	if !datastore.IsSQLite(f.h.db) {
		if err := f.h.db.Migrator().DropConstraint(&model.PaymentReceipt{}, "WalletEntry"); err != nil {
			t.Fatal(err)
		}
		for _, relation := range []string{"Entry", "ReversalEntry"} {
			if err := f.h.db.Migrator().DropConstraint(&model.WalletAdjustment{}, relation); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := f.h.db.Exec("DROP TABLE wallet_entries").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err == nil {
		t.Fatal("recorded financial schema was recreated as empty")
	}
	if f.h.db.Migrator().HasTable("wallet_entries") {
		t.Fatal("lost ledger was silently recreated")
	}
}

func TestWalletMigrationRepeatRetainsFinancialFacts(t *testing.T) {
	assertWalletRepeat(t, newOrderFixture(t))
}
func TestMySQLWalletMigrationRepeatRetainsFinancialFacts(t *testing.T) {
	assertWalletRepeat(t, newMySQLOrderFixture(t))
}
func assertWalletRepeat(t *testing.T, f orderFixture) {
	t.Helper()
	// Keep an ordinary pending business order across migration retries as well.
	original := f.create(t, 0)
	for i := 0; i < 2; i++ {
		if err := datastore.RunMigrations(f.h.db); err != nil {
			t.Fatal(err)
		}
	}
	if !f.h.db.Migrator().HasTable("wallet_accounts") || !f.h.db.Migrator().HasTable("wallet_entries") {
		t.Fatal("fresh schema lacks wallet tables")
	}
	var row model.Order
	if err := f.h.db.First(&row, original.ID).Error; err != nil || row.AmountCents != original.AmountCents || row.Status != "pending" || row.PlanSKUID != original.PlanSKUID {
		t.Fatalf("wallet migration rewrote a business order: %+v %v", row, err)
	}
	var count int64
	if err := f.h.db.Table("schema_migrations").Where("version = ?", "0038_wallet_topups.up.sql").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("wallet migration receipt: %d %v", count, err)
	}
}

func TestWalletMigrationCompletesInterruptedEmptySchema(t *testing.T) {
	assertWalletInterrupted(t, newOrderFixture(t), false)
}
func TestMySQLWalletMigrationCompletesInterruptedEmptySchema(t *testing.T) {
	assertWalletInterrupted(t, newMySQLOrderFixture(t), true)
}
func assertWalletInterrupted(t *testing.T, f orderFixture, mysql bool) {
	t.Helper()
	original := f.create(t, 0)
	if err := f.h.db.Exec("DELETE FROM schema_migrations WHERE version = ?", "0038_wallet_topups.up.sql").Error; err != nil {
		t.Fatal(err)
	}
	// Inject physical ledger loss in this isolated test database. MySQL
	// otherwise correctly prevents dropping the referenced financial table.
	if !datastore.IsSQLite(f.h.db) {
		if err := f.h.db.Migrator().DropConstraint(&model.PaymentReceipt{}, "WalletEntry"); err != nil {
			t.Fatal(err)
		}
		for _, relation := range []string{"Entry", "ReversalEntry"} {
			if err := f.h.db.Migrator().DropConstraint(&model.WalletAdjustment{}, relation); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := f.h.db.Exec("DROP TABLE wallet_entries").Error; err != nil {
		t.Fatal(err)
	}
	if mysql {
		if err := f.h.db.Exec("ALTER TABLE orders MODIFY COLUMN plan_sku_id bigint unsigned NOT NULL").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := datastore.RunMigrations(f.h.db); err != nil {
		t.Fatal(err)
	}
	if !f.h.db.Migrator().HasTable("wallet_entries") {
		t.Fatal("interrupted empty schema did not complete")
	}
	var retained model.Order
	if err := f.h.db.First(&retained, original.ID).Error; err != nil || retained.Status != "pending" || retained.AmountCents != original.AmountCents || retained.PlanSKUID != original.PlanSKUID {
		t.Fatalf("interrupted migration changed business history: %+v %v", retained, err)
	}
	createTopUp(t, f, 100, "after-interruption-topup")
}
