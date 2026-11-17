package handler

import (
	"encoding/json"
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

func TestAdministrativeWholeTerminationApprovesDefaultCustomAndZero(t *testing.T) {
	assertAdministrativeTermination(t, func(t *testing.T) orderFixture { return newOrderFixture(t) })
}
func TestMySQLAdministrativeWholeTerminationApprovesDefaultCustomAndZero(t *testing.T) {
	assertAdministrativeTermination(t, newMySQLBusinessOrderFixture)
}
func assertAdministrativeTermination(t *testing.T, fixture func(*testing.T) orderFixture) {
	t.Helper()
	for _, approval := range []int64{-1, 0, 3000, 9500, 10000} {
		t.Run(fmt.Sprint(approval), func(t *testing.T) {
			f, root := perpetualValueFixtureWithBase(t, fixture(t), 20) // real test wallet; actual10000, catalog12000, residual8000.
			admin := model.User{Email: "termination-approver@example.test", Password: "unused", Status: "active", IsAdmin: true}
			ordinary := model.User{Email: "termination-reader@example.test", Password: "unused", Status: "active"}
			if err := f.h.db.Create(&admin).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.h.db.Create(&ordinary).Error; err != nil {
				t.Fatal(err)
			}
			quote, err := f.h.services.SubscriptionValues.ReadForAdministrator(t.Context(), admin.ID, root.SubscriptionID)
			if err != nil || (quote.TotalMinor == nil || *quote.TotalMinor != 8000) || quote.MaxRefundMinor != 10000 || quote.UserID != 1 {
				t.Fatalf("admin quote %+v %v", quote, err)
			}
			if _, err := f.h.services.SubscriptionValues.ReadForAdministrator(t.Context(), ordinary.ID, root.SubscriptionID); !errors.Is(err, commerce.ErrOrderPermission) {
				t.Fatalf("ordinary actor read admin quote: %v", err)
			}
			in := commerce.AdministrativeSubscriptionTerminationRequest{SubscriptionTerminationRequest: commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "admin-whole-approved-refund"}, Reason: " administrator approved whole termination "}
			want := approval
			if approval < 0 {
				want = 8000
			} else {
				in.AmountMinor = &approval
			}
			if _, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), ordinary.ID, root.SubscriptionID, in); !errors.Is(err, commerce.ErrOrderPermission) {
				t.Fatalf("ordinary actor changed refund: %v", err)
			}
			if approval == 0 {
				trigger := `CREATE TRIGGER reject_admin_zero_audit BEFORE INSERT ON audit_logs WHEN NEW.action = 'subscription.unsubscribe' BEGIN SELECT RAISE(ABORT, 'admin audit unavailable'); END`
				if !datastore.IsSQLite(f.h.db) {
					trigger = `CREATE TRIGGER reject_admin_zero_audit BEFORE INSERT ON audit_logs FOR EACH ROW BEGIN IF NEW.action = 'subscription.unsubscribe' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'admin audit unavailable'; END IF; END`
				}
				if err := f.h.db.Exec(trigger).Error; err != nil {
					t.Fatal(err)
				}
				if _, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), admin.ID, root.SubscriptionID, in); err == nil {
					t.Fatal("unaudited zero termination committed")
				}
				var rollback model.Subscription
				if err := f.h.db.First(&rollback, root.SubscriptionID).Error; err != nil || rollback.Status != "active" || rollback.EndedAt != nil {
					t.Fatalf("zero termination partially committed %+v %v", rollback, err)
				}
				var count int64
				if err := f.h.db.Model(&model.SubscriptionValueSettlement{}).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("zero settlement survived rollback %d %v", count, err)
				}
				if err := f.h.db.Exec("DROP TRIGGER reject_admin_zero_audit").Error; err != nil {
					t.Fatal(err)
				}
			}
			excessive := int64(10001)
			bad := in
			bad.AmountMinor = &excessive
			if _, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), admin.ID, root.SubscriptionID, bad); !errors.Is(err, commerce.ErrSubscriptionSettlementConflict) {
				t.Fatalf("catalog discount became cash: %v", err)
			}
			negative := int64(-1)
			bad.AmountMinor = &negative
			if _, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), admin.ID, root.SubscriptionID, bad); err == nil {
				t.Fatal("negative approval accepted")
			}
			// Concurrent first submission and response replay share the same settlement.
			var wg sync.WaitGroup
			receipts := make(chan commerce.SubscriptionTerminationReceipt, 8)
			errs := make(chan error, 8)
			for range 8 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					receipt, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), admin.ID, root.SubscriptionID, in)
					receipts <- receipt
					errs <- err
				}()
			}
			wg.Wait()
			close(receipts)
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatalf("concurrent approval %v", err)
				}
			}
			var receiptID uint
			for receipt := range receipts {
				if receipt.AmountMinor != want || receipt.Status != "canceled" || receipt.RevocationStatus != "pending_verification" || receiptID != 0 && receipt.ID != receiptID || want == 0 && receipt.WalletEntryID != nil {
					t.Fatalf("approved receipt %+v", receipt)
				}
				receiptID = receipt.ID
			}
			var row model.SubscriptionValueSettlement
			if err := f.h.db.First(&row, receiptID).Error; err != nil || row.UserID != 1 || row.ActorID != admin.ID || row.AmountMinor != want || row.Reason != strings.TrimSpace(in.Reason) {
				t.Fatalf("approval identity/audit %+v %v", row, err)
			}
			var evidence struct {
				Value   commerce.ValueProjection
				Sources map[uint]int64
			}
			if err := json.Unmarshal([]byte(row.QuoteSnapshot), &evidence); err != nil || evidence.Value.TotalMinor != 8000 {
				t.Fatalf("default residual lost %+v %v", evidence, err)
			}
			var audit model.AuditLog
			if err := f.h.db.Where("action = ? AND target = ?", "subscription.unsubscribe", fmt.Sprintf("subscription:%d", root.SubscriptionID)).First(&audit).Error; err != nil || audit.UserID == nil || *audit.UserID != admin.ID || !strings.Contains(audit.Detail, "default_minor=8000") || !strings.Contains(audit.Detail, fmt.Sprintf("actual_minor=%d", want)) || !strings.Contains(audit.Detail, strings.TrimSpace(in.Reason)) {
				t.Fatalf("approval audit %+v %v", audit, err)
			}
			var wallet model.WalletAccount
			if err := f.h.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 20000+want || wallet.ReservedMinor != 0 {
				t.Fatalf("approved refund money %+v %v", wallet, err)
			}
			var refunds int64
			if err := f.h.db.Model(&model.WalletEntry{}).Where("user_id = ? AND kind = ?", 1, "subscription_refund").Count(&refunds).Error; err != nil || want == 0 && refunds != 0 || want > 0 && refunds != 1 {
				t.Fatalf("zero/duplicate refund entry %d %v", refunds, err)
			}
			if want > 0 {
				var entry model.WalletEntry
				if err := f.h.db.First(&entry, *row.EntryID).Error; err != nil || !strings.Contains(entry.Reason, "管理员整份退订退款") || !strings.Contains(entry.Reason, strings.TrimSpace(in.Reason)) {
					t.Fatalf("administrative wallet reason %+v %v", entry, err)
				}
			}
			var sub model.Subscription
			if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil || sub.Status != "canceled" || sub.EndedAt == nil || sub.FlowUsed != 20 {
				t.Fatalf("approval retained rights %+v %v", sub, err)
			}
			var live int64
			if err := f.h.db.Model(&model.ProtocolCredential{}).Where("subscription_id = ? AND status <> ?", sub.ID, "revoked").Count(&live).Error; err != nil || live != 0 {
				t.Fatalf("approval kept credentials %d %v", live, err)
			}
			changed := in
			changed.Reason = "a different approval"
			if _, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), admin.ID, sub.ID, changed); !errors.Is(err, commerce.ErrSubscriptionSettlementConflict) {
				t.Fatalf("changed approval replay %v", err)
			}
			// Permission is checked again even when a successful receipt already exists.
			if err := f.h.db.Model(&admin).Update("is_admin", false).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), admin.ID, sub.ID, in); !errors.Is(err, commerce.ErrOrderPermission) {
				t.Fatalf("revoked administrator replay %v", err)
			}
		})
	}
}

func TestAdministrativeTerminationHTTPSeparatesOrdinaryMoneyAndPermission(t *testing.T) {
	f := newOrderFixture(t)
	ordinary := model.User{Email: "ordinary-termination-http@example.test", Password: "unused", Status: "active"}
	if err := f.h.db.Create(&ordinary).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := f.h.issueToken(authClaims{UserID: ordinary.ID, Email: ordinary.Email})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		token, body string
		status      int
	}{{"", "{}", 401}, {token, "{}", 403}, {f.token, `{"quote_fingerprint":"` + strings.Repeat("a", 64) + `","idempotency_key":"admin-http-termination","amount_minor":0,"reason":"approved","currency":"USD"}`, 400}} {
		out := httptest.NewRecorder()
		f.h.AdminSubscriptionValueHandler(out, announcementRequest("POST", "/api/v1/admin/subscriptions/999999/unsubscribe", tc.token, tc.body))
		if out.Code != tc.status || out.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("admin HTTP %d %s", out.Code, out.Body.String())
		}
	}
}

func TestAdministrativeTerminationHTTPAcceptsExplicitZero(t *testing.T) {
	f, root := perpetualValueFixtureWithBase(t, newOrderFixture(t), 20)
	quoteResponse := httptest.NewRecorder()
	f.h.AdminSubscriptionValueHandler(quoteResponse, announcementRequest("GET", fmt.Sprintf("/api/v1/admin/subscriptions/%d/value", root.SubscriptionID), f.token, ""))
	if quoteResponse.Code != 200 {
		t.Fatalf("admin quote HTTP %d %s", quoteResponse.Code, quoteResponse.Body.String())
	}
	var envelope struct {
		Data commerce.AdministrativeSubscriptionValueQuote `json:"data"`
	}
	if err := json.Unmarshal(quoteResponse.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"quote_fingerprint":%q,"idempotency_key":"admin-http-zero-approved","amount_minor":0,"reason":"administrator waived refund"}`, envelope.Data.Fingerprint)
	for range 2 {
		out := httptest.NewRecorder()
		f.h.AdminSubscriptionValueHandler(out, announcementRequest("POST", fmt.Sprintf("/api/v1/admin/subscriptions/%d/unsubscribe", root.SubscriptionID), f.token, body))
		if out.Code != 200 {
			t.Fatalf("zero HTTP %d %s", out.Code, out.Body.String())
		}
		var receipt struct {
			Data commerce.SubscriptionTerminationReceipt `json:"data"`
		}
		if err := json.Unmarshal(out.Body.Bytes(), &receipt); err != nil || receipt.Data.AmountMinor != 0 || receipt.Data.WalletEntryID != nil || receipt.Data.Status != "canceled" {
			t.Fatalf("zero HTTP receipt %+v %v", receipt, err)
		}
	}
}
