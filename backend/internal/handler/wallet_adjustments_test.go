package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func adjustmentTicket(t testing.TB, f orderFixture) model.Ticket {
	t.Helper()
	row := model.Ticket{TicketNo: "wallet-adjustment-ticket", UserID: 1, Subject: "人工核对", Category: "billing", LastMessageAt: time.Now().UTC()}
	if err := f.h.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row
}
func TestControlledWalletAdjustment(t *testing.T) {
	assertControlledWalletAdjustment(t, newOrderFixture(t))
}
func TestMySQLControlledWalletAdjustment(t *testing.T) {
	assertControlledWalletAdjustment(t, newMySQLOrderFixture(t))
}
func assertControlledWalletAdjustment(t *testing.T, f orderFixture) {
	fundTestWallet(t, f, 1000)
	priceForFunding(t, f, 100)
	order := f.create(t, 0)
	if _, err := fundingService(f).Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	ticket := adjustmentTicket(t, f)
	s := f.h.services.WalletAdjustments
	req := commerce.WalletDebitRequest{IdempotencyKey: "controlled-debit-1", UserID: 1, Currency: "CNY", AmountMinor: 901, TicketID: ticket.ID, Reason: "工单人工退款出款"}
	if _, err := s.Debit(t.Context(), 1, req); err == nil {
		t.Fatal("deducted reserved money")
	}
	walletMatches(t, f, 1000, 100, 2)
	req.AmountMinor = 900
	result, err := s.Debit(t.Context(), 1, req)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		replay, e := s.Debit(t.Context(), 1, req)
		if e != nil || replay.ID != result.ID {
			t.Fatalf("repeated debit: %+v %v", replay, e)
		}
	}
	walletMatches(t, f, 100, 100, 3)
	var entry model.WalletEntry
	if err := f.h.db.First(&entry, result.EntryID).Error; err != nil || entry.SourceOrderID != nil || entry.BalanceDelta != -900 {
		t.Fatalf("invented source order: %+v %v", entry, err)
	}
	changed := req
	changed.Reason = "changed reason"
	if _, err := s.Debit(t.Context(), 1, changed); !errors.Is(err, commerce.ErrWalletAdjustmentConflict) {
		t.Fatalf("changed debit: %v", err)
	}
	if _, err := s.Reverse(t.Context(), 1, result.ID, commerce.WalletReversalRequest{Reason: "尚未核对"}); !errors.Is(err, commerce.ErrWalletAdjustmentConflict) {
		t.Fatal("pending result restored cash")
	}
	report := commerce.WalletPayoutRequest{IdempotencyKey: "manual-payout-result-1", Outcome: "unknown", Note: "private bank investigation", OccurredAt: time.Now().UTC()}
	if _, err := s.RecordPayout(t.Context(), 1, result.ID, report); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reverse(t.Context(), 1, result.ID, commerce.WalletReversalRequest{Reason: "尚未核对"}); !errors.Is(err, commerce.ErrWalletAdjustmentConflict) {
		t.Fatal("unknown payout reversed")
	}
	report.IdempotencyKey = "manual-payout-result-2"
	report.Outcome = "failed"
	report.Reference = "private-bank-reference"
	for i := 0; i < 3; i++ {
		if _, err := s.RecordPayout(t.Context(), 1, result.ID, report); err != nil {
			t.Fatal(err)
		}
	}
	walletMatches(t, f, 100, 100, 3)
	opposite := report
	opposite.IdempotencyKey = "contradictory-result"
	opposite.Outcome = "succeeded"
	if _, err := s.RecordPayout(t.Context(), 1, result.ID, opposite); !errors.Is(err, commerce.ErrWalletAdjustmentConflict) {
		t.Fatal("contradictory final result accepted")
	}
	for i := 0; i < 3; i++ {
		out, e := s.Reverse(t.Context(), 1, result.ID, commerce.WalletReversalRequest{Reason: "银行确认未出款，恢复余额"})
		if e != nil || out.ReversalEntryID == nil {
			t.Fatalf("reversal: %+v %v", out, e)
		}
	}
	walletMatches(t, f, 1000, 100, 4)
	read, err := s.Read(t.Context(), 1, result.ID)
	if err != nil || len(read.Results) != 2 || read.ReversalEntryID == nil {
		t.Fatalf("audit history: %+v %v", read, err)
	}
	out := httptest.NewRecorder()
	f.h.WalletHandler(out, announcementRequest("GET", "/api/v1/account/wallet?currency=CNY", f.token, ""))
	if out.Code != 200 || strings.Contains(out.Body.String(), "private-bank") || strings.Contains(out.Body.String(), "private bank") {
		t.Fatalf("private payout data leaked: %s", out.Body.String())
	}
	for i := 0; i < 2; i++ {
		if err := datastore.RunMigrations(f.h.db); err != nil {
			t.Fatal(err)
		}
	}
	walletMatches(t, f, 1000, 100, 4)
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Debit(t.Context(), 1, req); !errors.Is(err, commerce.ErrOrderPermission) {
		t.Fatalf("stale administrator debit: %v", err)
	}
	if _, err := s.Read(t.Context(), 1, result.ID); !errors.Is(err, commerce.ErrOrderPermission) {
		t.Fatalf("stale administrator read: %v", err)
	}
}
func TestControlledWalletAdjustmentRollback(t *testing.T) {
	assertAdjustmentRollback(t, newOrderFixture(t))
}
func TestMySQLControlledWalletAdjustmentRollback(t *testing.T) {
	assertAdjustmentRollback(t, newMySQLBusinessOrderFixture(t))
}
func assertAdjustmentRollback(t *testing.T, f orderFixture) {
	fundTestWallet(t, f, 1000)
	ticket := adjustmentTicket(t, f)
	sql := `CREATE TRIGGER reject_adjustment BEFORE INSERT ON wallet_adjustments BEGIN SELECT RAISE(ABORT, 'case unavailable'); END`
	if !datastore.IsSQLite(f.h.db) {
		sql = `CREATE TRIGGER reject_adjustment BEFORE INSERT ON wallet_adjustments FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'case unavailable'`
	}
	if err := f.h.db.Exec(sql).Error; err != nil {
		t.Fatal(err)
	}
	req := commerce.WalletDebitRequest{IdempotencyKey: "rollback-adjustment", UserID: 1, Currency: "CNY", AmountMinor: 500, TicketID: ticket.ID, Reason: "工单核对出款"}
	if _, err := f.h.services.WalletAdjustments.Debit(t.Context(), 1, req); err == nil {
		t.Fatal("case failure acknowledged")
	}
	walletMatches(t, f, 1000, 0, 1)
	if err := f.h.db.Exec("DROP TRIGGER reject_adjustment").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.WalletAdjustments.Debit(t.Context(), 1, req); err != nil {
		t.Fatal(err)
	}
	walletMatches(t, f, 500, 0, 2)
}
func TestMySQLControlledWalletAdjustmentConcurrent(t *testing.T) {
	f := newMySQLBusinessOrderFixture(t)
	fundTestWallet(t, f, 1000)
	ticket := adjustmentTicket(t, f)
	s := f.h.services.WalletAdjustments
	req := commerce.WalletDebitRequest{IdempotencyKey: "concurrent-adjustment", UserID: 1, Currency: "CNY", AmountMinor: 900, TicketID: ticket.ID, Reason: "人工核对出款"}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Debit(t.Context(), 1, req); results <- err }()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent debit: %v", err)
		}
	}
	walletMatches(t, f, 100, 0, 2)
	page, err := s.List(t.Context(), 1, 1, "CNY", 0, 25)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("debit cases %+v %v", page, err)
	}
	id := page.Items[0].ID
	if _, err := s.RecordPayout(t.Context(), 1, id, commerce.WalletPayoutRequest{IdempotencyKey: "concurrent-failed-report", Outcome: "failed", Note: "人工核对银行拒绝出款", OccurredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	results = make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Reverse(t.Context(), 1, id, commerce.WalletReversalRequest{Reason: "银行确认失败"})
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
	walletMatches(t, f, 1000, 0, 3)
	var count int64
	f.h.db.Model(&model.AuditLog{}).Where("action IN ?", []string{"wallet.adjustment.debit", "wallet.adjustment.reverse"}).Count(&count)
	if count != 2 {
		t.Fatalf("duplicated audit %d", count)
	}
}

func TestControlledWalletAdjustmentHTTPAndTicketBoundaries(t *testing.T) {
	f := newOrderFixture(t)
	fundTestWallet(t, f, 1000)
	ticket := adjustmentTicket(t, f)
	for _, body := range []string{
		`{"user_id":1,"currency":"CNY","amount_minor":100,"ticket_id":99999,"reason":"missing ticket","idempotency_key":"missing-ticket-request"}`,
		`{"user_id":1,"currency":"CNY","amount_minor":100,"ticket_id":1,"reason":"forged actor","idempotency_key":"forged-actor-request","actor_id":2}`,
		`{"user_id":1,"currency":"CNY","amount_minor":-1,"ticket_id":1,"reason":"negative amount","idempotency_key":"negative-amount-request"}`,
	} {
		out := httptest.NewRecorder()
		f.h.AdminWalletAdjustmentsHandler(out, announcementRequest("POST", "/api/v1/admin/wallet-adjustments", f.token, body))
		if out.Code != 400 {
			t.Fatalf("invalid administrative write accepted: %d %s", out.Code, out.Body.String())
		}
	}
	otherUser := model.User{Email: "adjustment-other@example.test", Password: "unused", Status: "active"}
	if err := f.h.db.Create(&otherUser).Error; err != nil {
		t.Fatal(err)
	}
	otherTicket := model.Ticket{TicketNo: "other-user-adjustment", UserID: otherUser.ID, Subject: "另一个账户", Category: "billing", LastMessageAt: time.Now().UTC()}
	if err := f.h.db.Create(&otherTicket).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.WalletAdjustments.Debit(t.Context(), 1, commerce.WalletDebitRequest{IdempotencyKey: "wrong-ticket-owner", UserID: 1, Currency: "CNY", AmountMinor: 100, TicketID: otherTicket.ID, Reason: "工单归属校验"}); err == nil {
		t.Fatal("accepted another account's ticket")
	}

	out := httptest.NewRecorder()
	f.h.AdminUserWalletHandler(out, announcementRequest("GET", "/api/v1/admin/users/1/wallet?currency=CNY", f.token, ""))
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"balance_minor":"1000"`) {
		t.Fatalf("administrative wallet: %d %s", out.Code, out.Body.String())
	}
	// A confirmed successful payout never allows a second cash credit.
	req := commerce.WalletDebitRequest{IdempotencyKey: "successful-payout-debit", UserID: 1, Currency: "CNY", AmountMinor: 100, TicketID: ticket.ID, Reason: "工单实际出款"}
	s := f.h.services.WalletAdjustments
	row, err := s.Debit(t.Context(), 1, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordPayout(t.Context(), 1, row.ID, commerce.WalletPayoutRequest{IdempotencyKey: "successful-payout-report", Outcome: "succeeded", Reference: "bank-success-reference", OccurredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reverse(t.Context(), 1, row.ID, commerce.WalletReversalRequest{Reason: "不能重复返款"}); !errors.Is(err, commerce.ErrWalletAdjustmentConflict) {
		t.Fatal("successful payout reversed")
	}
	walletMatches(t, f, 900, 0, 2)
}
func TestWalletAdjustmentSchemaRecovery(t *testing.T) {
	assertAdjustmentSchemaRecovery(t, newOrderFixture(t))
}
func TestMySQLWalletAdjustmentSchemaRecovery(t *testing.T) {
	assertAdjustmentSchemaRecovery(t, newMySQLOrderFixture(t))
}
func assertAdjustmentSchemaRecovery(t *testing.T, f orderFixture) {
	if err := f.h.db.Exec("DROP TABLE wallet_payout_results").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err == nil {
		t.Fatal("lost payout history silently recreated")
	}
	if f.h.db.Migrator().HasTable("wallet_payout_results") {
		t.Fatal("lost financial evidence replaced by empty schema")
	}
	if err := f.h.db.Exec("DELETE FROM schema_migrations WHERE version = ?", "0041_wallet_adjustments.up.sql").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err != nil {
		t.Fatalf("empty interrupted DDL did not complete: %v", err)
	}
	if !f.h.db.Migrator().HasTable("wallet_payout_results") {
		t.Fatal("interrupted migration missing table")
	}
	fundTestWallet(t, f, 1000)
	ticket := adjustmentTicket(t, f)
	if _, err := f.h.services.WalletAdjustments.Debit(t.Context(), 1, commerce.WalletDebitRequest{IdempotencyKey: "recovered-schema-debit", UserID: 1, Currency: "CNY", AmountMinor: 100, TicketID: ticket.ID, Reason: "工单核对出款"}); err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Exec("DROP TABLE wallet_payout_results").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Exec("DELETE FROM schema_migrations WHERE version = ?", "0041_wallet_adjustments.up.sql").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err == nil {
		t.Fatal("incomplete financial schema with retained debits accepted")
	}
	if f.h.db.Migrator().HasTable("wallet_payout_results") {
		t.Fatal("incomplete live financial schema silently filled")
	}
}
