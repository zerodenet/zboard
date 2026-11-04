package handler

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestAdminWalletCredit(t *testing.T) { assertAdminWalletCredit(t, newOrderFixture(t)) }
func TestMySQLAdminWalletCredit(t *testing.T) {
	assertAdminWalletCredit(t, newMySQLBusinessOrderFixture(t))
}
func assertAdminWalletCredit(t *testing.T, f orderFixture) {
	fundTestWallet(t, f, 1000)
	priceForFunding(t, f, 100)
	order := f.create(t, 0)
	if _, err := fundingService(f).Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	var originalOrders int64
	f.h.db.Model(&model.Order{}).Count(&originalOrders)
	s := f.h.services.WalletAdjustments
	in := commerce.WalletCreditRequest{UserID: 1, Currency: " cny ", AmountMinor: 250, Reason: " 活动赠送 ", IdempotencyKey: "admin-credit-gift"}
	entry, err := s.Credit(t.Context(), 1, in)
	if err != nil || entry.Kind != "admin_credit" || entry.SourceOrderID != nil || entry.BalanceDelta != 250 {
		t.Fatal(entry, err)
	}
	for i := 0; i < 3; i++ {
		replay, err := s.Credit(t.Context(), 1, in)
		if err != nil || replay.ID != entry.ID {
			t.Fatal(replay, err)
		}
	}
	walletMatches(t, f, 1250, 100, 3)
	for _, changed := range []commerce.WalletCreditRequest{
		{UserID: 1, Currency: "CNY", AmountMinor: 251, Reason: "活动赠送", IdempotencyKey: in.IdempotencyKey},
		{UserID: 1, Currency: "CNY", AmountMinor: 250, Reason: "不同的原因", IdempotencyKey: in.IdempotencyKey},
		{UserID: 1, Currency: "USD", AmountMinor: 250, Reason: "活动赠送", IdempotencyKey: in.IdempotencyKey},
		{UserID: 999, Currency: "CNY", AmountMinor: 250, Reason: "活动赠送", IdempotencyKey: in.IdempotencyKey},
	} {
		if _, err := s.Credit(t.Context(), 1, changed); !errors.Is(err, commerce.ErrWalletAdjustmentConflict) {
			t.Fatalf("changed intent accepted: %v", err)
		}
	}
	var audits int64
	if err := f.h.db.Model(&model.AuditLog{}).Where("action = ?", "wallet.adjustment.credit").Count(&audits).Error; err != nil || audits != 1 {
		t.Fatal("audit duplicated", audits, err)
	}
	var adjustments, orders int64
	f.h.db.Model(&model.WalletAdjustment{}).Count(&adjustments)
	f.h.db.Model(&model.Order{}).Count(&orders)
	if adjustments != 0 || orders != originalOrders {
		t.Fatal("credit fabricated a payout or order", adjustments, orders)
	}
	other := model.User{Email: "credit-target@example.test", Password: "unused", Status: "disabled"}
	if err := f.h.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	in.UserID, in.IdempotencyKey = other.ID, "admin-credit-disabled-target"
	if _, err := s.Credit(t.Context(), 1, in); err != nil {
		t.Fatal("disabled user cannot be managed", err)
	}
	if err := f.h.db.Delete(&other).Error; err != nil {
		t.Fatal(err)
	}
	in.IdempotencyKey = "admin-credit-deleted-target"
	if _, err := s.Credit(t.Context(), 1, in); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatal("deleted target credited", err)
	}
	if err := f.h.db.Model(&model.User{}).Where("id = 1").Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	in.UserID, in.IdempotencyKey = 1, "admin-credit-gift"
	if _, err := s.Credit(t.Context(), 1, in); !errors.Is(err, commerce.ErrOrderPermission) {
		t.Fatal("stale administrator replay allowed", err)
	}
}

func TestAdminWalletCreditHTTPAndRollback(t *testing.T) {
	f := newOrderFixture(t)
	for i, body := range []string{
		`{"user_id":1,"currency":"CNY","amount_minor":-1,"reason":"赠送余额","idempotency_key":"negative-credit"}`,
		`{"user_id":1,"currency":"CNY","amount_minor":0,"reason":"赠送余额","idempotency_key":"zero-credit"}`,
		`{"user_id":1,"currency":"CNY","amount_minor":1.5,"reason":"赠送余额","idempotency_key":"fraction-credit"}`,
		`{"user_id":1,"currency":"CNY","amount_minor":9007199254740992,"reason":"赠送余额","idempotency_key":"large-credit"}`,
		`{"user_id":1,"currency":"CNY","amount_minor":100,"reason":"赠送余额","idempotency_key":"forged-credit","actor_id":2}`,
	} {
		w := httptest.NewRecorder()
		f.h.AdminWalletCreditHandler(w, announcementRequest("POST", "/api/v1/admin/wallet-credits", f.token, body))
		if w.Code != 400 {
			t.Fatalf("invalid credit %d accepted: %d %s", i, w.Code, w.Body.String())
		}
	}
	in := commerce.WalletCreditRequest{UserID: 1, Currency: "CNY", AmountMinor: 1234, Reason: "管理员赠送", IdempotencyKey: "audit-atomic-credit"}
	if err := f.h.db.Exec(`CREATE TRIGGER reject_credit_audit BEFORE INSERT ON audit_logs WHEN NEW.action = 'wallet.adjustment.credit' BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.WalletAdjustments.Credit(t.Context(), 1, in); err == nil {
		t.Fatal("credit without audit committed")
	}
	walletMatches(t, f, 0, 0, 0)
	if err := f.h.db.Exec("DROP TRIGGER reject_credit_audit").Error; err != nil {
		t.Fatal(err)
	}
	body := `{"user_id":1,"currency":"CNY","amount_minor":1234,"reason":"管理员赠送","idempotency_key":"audit-atomic-credit"}`
	w := httptest.NewRecorder()
	f.h.AdminWalletCreditHandler(w, announcementRequest("POST", "/api/v1/admin/wallet-credits", f.token, body))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"balance_delta":"1234"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	walletMatches(t, f, 1234, 0, 1)
	// Loss of the materialized account is not permission to create a fresh zero wallet.
	if err := f.h.db.Exec("PRAGMA foreign_keys=OFF").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Exec("DELETE FROM wallet_accounts WHERE user_id=1").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Exec("PRAGMA foreign_keys=ON").Error; err != nil {
		t.Fatal(err)
	}
	in.IdempotencyKey = "missing-summary-credit"
	if _, err := f.h.services.WalletAdjustments.Credit(t.Context(), 1, in); !errors.Is(err, commerce.ErrWalletUnavailable) {
		t.Fatal("lost summary replaced", err)
	}
}

func TestMySQLAdminWalletCreditConcurrent(t *testing.T) {
	f := newMySQLBusinessOrderFixture(t)
	in := commerce.WalletCreditRequest{UserID: 1, Currency: "CNY", AmountMinor: 100, Reason: "并发赠送", IdempotencyKey: "concurrent-admin-credit"}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.h.services.WalletAdjustments.Credit(t.Context(), 1, in)
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
	walletMatches(t, f, 100, 0, 1)
	var audits int64
	f.h.db.Model(&model.AuditLog{}).Where("action = ?", "wallet.adjustment.credit").Count(&audits)
	if audits != 1 {
		t.Fatal(fmt.Sprintf("%d audits for one credit", audits))
	}
	if err := datastore.RunMigrations(f.h.db); err != nil {
		t.Fatal(err)
	}
	walletMatches(t, f, 100, 0, 1)
}
