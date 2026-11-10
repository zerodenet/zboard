package handler

import (
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestAdminWalletDirectDebit(t *testing.T) { assertAdminWalletDirectDebit(t, newOrderFixture(t)) }
func TestMySQLAdminWalletDirectDebit(t *testing.T) {
	assertAdminWalletDirectDebit(t, newMySQLBusinessOrderFixture(t))
}
func assertAdminWalletDirectDebit(t *testing.T, f orderFixture) {
	fundTestWallet(t, f, 1000)
	priceForFunding(t, f, 100)
	order := f.create(t, 0)
	if _, err := fundingService(f).Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	var originalOrders int64
	f.h.db.Model(&model.Order{}).Count(&originalOrders)
	s := f.h.services.WalletAdjustments
	in := commerce.WalletBalanceRequest{UserID: 1, Currency: " cny ", AmountMinor: 900, Reason: " 纠正余额 ", IdempotencyKey: "direct-debit-original"}
	entry, err := s.DirectDebit(t.Context(), 1, in)
	if err != nil || entry.Kind != "admin_debit" || entry.BalanceDelta != -900 || entry.ReservedDelta != 0 || entry.SourceOrderID != nil {
		t.Fatal(entry, err)
	}
	// Retry still succeeds after available balance has fallen to zero.
	for i := 0; i < 3; i++ {
		replay, err := s.DirectDebit(t.Context(), 1, in)
		if err != nil || replay.ID != entry.ID {
			t.Fatal(replay, err)
		}
	}
	walletMatches(t, f, 100, 100, 3)
	for _, mutate := range []func(*commerce.WalletBalanceRequest){
		func(v *commerce.WalletBalanceRequest) { v.AmountMinor = 899 },
		func(v *commerce.WalletBalanceRequest) { v.Reason = "另一个原因" },
		func(v *commerce.WalletBalanceRequest) { v.UserID = 999 },
		func(v *commerce.WalletBalanceRequest) { v.Currency = "USD" },
	} {
		changed := in
		mutate(&changed)
		if _, err := s.DirectDebit(t.Context(), 1, changed); !errors.Is(err, commerce.ErrWalletAdjustmentConflict) {
			t.Fatalf("changed request accepted: %v", err)
		}
	}
	in.IdempotencyKey = "direct-debit-reserved"
	in.AmountMinor = 1
	if _, err := s.DirectDebit(t.Context(), 1, in); err == nil || !strings.Contains(err.Error(), "可用余额不足") {
		t.Fatal("reserved funds were debited", err)
	}
	walletMatches(t, f, 100, 100, 3)
	var audits, adjustments, orders int64
	f.h.db.Model(&model.AuditLog{}).Where("action = ?", "wallet.adjustment.debit").Count(&audits)
	f.h.db.Model(&model.WalletAdjustment{}).Count(&adjustments)
	f.h.db.Model(&model.Order{}).Count(&orders)
	if audits != 1 || adjustments != 0 || orders != originalOrders {
		t.Fatal("unexpected audit/payout/order", audits, adjustments, orders)
	}
	// An empty wallet in another currency cannot spend this currency's funds.
	in.Currency = "USD"
	in.IdempotencyKey = "direct-debit-empty-currency"
	if _, err := s.DirectDebit(t.Context(), 1, in); err == nil {
		t.Fatal("empty currency debited")
	}
	var usd int64
	f.h.db.Model(&model.WalletAccount{}).Where("user_id=1 AND currency='USD'").Count(&usd)
	if usd != 0 {
		t.Fatal("failed debit left a wallet behind")
	}
	// A stale operator cannot replay an earlier authorized debit.
	if err := f.h.db.Model(&model.User{}).Where("id=1").Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	in.Currency = "CNY"
	in.AmountMinor = 900
	in.IdempotencyKey = "direct-debit-original"
	if _, err := s.DirectDebit(t.Context(), 1, in); !errors.Is(err, commerce.ErrOrderPermission) {
		t.Fatal("stale admin replay allowed", err)
	}
}

func TestAdminWalletDirectDebitHTTPAndRollback(t *testing.T) {
	f := newOrderFixture(t)
	fundTestWallet(t, f, 1000)
	for _, body := range []string{
		`{"user_id":1,"currency":"CNY","amount_minor":0,"reason":"调整","idempotency_key":"zero-debit"}`,
		`{"user_id":1,"currency":"CNY","amount_minor":-1,"reason":"调整","idempotency_key":"negative-debit"}`,
		`{"user_id":1,"currency":"CNY","amount_minor":1.5,"reason":"调整","idempotency_key":"fraction-debit"}`,
		`{"user_id":1,"currency":"CNY","amount_minor":9007199254740992,"reason":"调整","idempotency_key":"large-debit"}`,
		`{"user_id":1,"currency":"CNY","amount_minor":1,"reason":" ","idempotency_key":"blank-debit"}`,
		`{"user_id":1,"currency":"CNY","amount_minor":1,"reason":"调整","idempotency_key":"forged-debit","actor_id":2}`,
	} {
		w := httptest.NewRecorder()
		f.h.AdminWalletDirectDebitHandler(w, announcementRequest("POST", "/api/v1/admin/wallet-debits", f.token, body))
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	body := `{"user_id":1,"currency":"CNY","amount_minor":250,"reason":"纠正余额","idempotency_key":"atomic-direct-debit"}`
	if err := f.h.db.Exec(`CREATE TRIGGER reject_debit_audit BEFORE INSERT ON audit_logs WHEN NEW.action = 'wallet.adjustment.debit' BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	f.h.AdminWalletDirectDebitHandler(w, announcementRequest("POST", "/api/v1/admin/wallet-debits", f.token, body))
	if w.Code == 200 {
		t.Fatal("debit without audit committed")
	}
	walletMatches(t, f, 1000, 0, 1)
	f.h.db.Exec("DROP TRIGGER reject_debit_audit")
	w = httptest.NewRecorder()
	f.h.AdminWalletDirectDebitHandler(w, announcementRequest("POST", "/api/v1/admin/wallet-debits", f.token, body))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"balance_delta":"-250"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	walletMatches(t, f, 750, 0, 2)
	f.h.db.Model(&model.User{}).Where("id=1").Update("is_admin", false)
	w = httptest.NewRecorder()
	f.h.AdminWalletDirectDebitHandler(w, announcementRequest("POST", "/api/v1/admin/wallet-debits", f.token, body))
	if w.Code != 403 {
		t.Fatal("non-admin debit accepted", w.Code, w.Body.String())
	}
}

func TestMySQLAdminWalletDirectDebitConcurrent(t *testing.T) {
	f := newMySQLBusinessOrderFixture(t)
	fundTestWallet(t, f, 1000)
	in := commerce.WalletBalanceRequest{UserID: 1, Currency: "CNY", AmountMinor: 900, Reason: "并发调整", IdempotencyKey: "concurrent-direct-debit"}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.h.services.WalletAdjustments.DirectDebit(t.Context(), 1, in)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	walletMatches(t, f, 100, 0, 2)
	var audits int64
	f.h.db.Model(&model.AuditLog{}).Where("action=?", "wallet.adjustment.debit").Count(&audits)
	if audits != 1 {
		t.Fatal(audits)
	}
}
