package handler

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestPaymentReceiptOwnsCashAcrossEventsAndChannels(t *testing.T) {
	assertPaymentReceipts(t, newOrderFixture(t))
}
func TestMySQLPaymentReceiptOwnsCashAcrossEventsAndChannels(t *testing.T) {
	assertPaymentReceipts(t, newMySQLOrderFixture(t))
}
func receiptService(f orderFixture) commerce.PaymentFacts {
	return commerce.PaymentFacts{Repository: commercestore.PaymentFacts{DB: f.h.db, Issuer: zeroadapter.Issuer{Cipher: f.h.credentialCipher}}}
}
func prepareReceiptAttempt(t testing.TB, f orderFixture, order uint, provider string) {
	t.Helper()
	_, permitted, err := f.h.services.PaymentCheckout.Open(t.Context(), 1, order, provider)
	if err != nil || !permitted {
		t.Fatalf("checkout admission: %v %v", permitted, err)
	}
}
func assertPaymentReceipts(t *testing.T, f orderFixture) {
	t.Helper()
	attachOrderPublishEndpoint(t, f)
	order := f.create(t, 0)
	for _, provider := range []string{"test-channel-a", "test-channel-b"} {
		prepareReceiptAttempt(t, f, order.ID, provider)
	}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	facts := receiptService(f)
	authority := commerce.PaymentFactAuthority{AccountID: 1, Provider: "test-channel-a", RequireAttempt: true}
	fact := commerce.PaymentFact{OrderID: order.ID, ProviderEventID: "first-event", ProviderTradeNo: "trade-a", Status: "paid", AmountMinor: order.PayableAmount, Currency: order.Currency, OccurredAt: time.Now().UTC()}
	unprepared := authority
	unprepared.Provider = "unprepared-channel"
	if _, err := facts.Record(t.Context(), unprepared, fact); !errors.Is(err, commerce.ErrPaymentFactConflict) {
		t.Fatalf("unprepared plugin collected: %v", err)
	}
	first, err := facts.Record(t.Context(), authority, fact)
	if err != nil || !first.Fulfilled || first.Disposition != "fulfilled" {
		t.Fatalf("first receipt: %+v %v", first, err)
	}
	for i := 0; i < 2; i++ {
		fact.ProviderEventID = fmt.Sprintf("same-trade-event-%d", i)
		again, err := facts.Record(t.Context(), authority, fact)
		if err != nil || again.Fulfilled || again.Disposition != "fulfilled" {
			t.Fatalf("same cash repeated: %+v %v", again, err)
		}
	}
	if _, permitted, err := f.h.services.PaymentCheckout.Open(t.Context(), 1, order.ID, "fresh-after-paid"); err != nil || permitted {
		t.Fatalf("new plugin gained authority after payment: %v %v", permitted, err)
	}
	authority.Provider, fact.ProviderTradeNo = "test-channel-b", "second-real-trade"
	var group sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			current := fact
			current.ProviderEventID = fmt.Sprintf("parallel-receipt-%d", i)
			result, err := facts.Record(t.Context(), authority, current)
			if err == nil && (result.Fulfilled || result.Disposition != "wallet_credit") {
				err = fmt.Errorf("extra cash repeated fulfillment: %+v", result)
			}
			failures <- err
		}(i)
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	wallet, err := f.h.services.Wallet.Read(t.Context(), 1, "CNY", 0, 50)
	if err != nil || wallet.BalanceMinor != order.PayableAmount || len(wallet.Entries) != 1 {
		t.Fatalf("duplicate actual payment lost or repeated: %+v %v", wallet, err)
	}
	for table, want := range map[string]int64{"payment_receipts": 2, "subscriptions": 1, "protocol_credentials": 1, "quota_events": 1, "node_config_publishes": 1} {
		var count int64
		if err := f.h.db.Table(table).Count(&count).Error; err != nil || count != want {
			t.Fatalf("%s count %d want %d: %v", table, count, want, err)
		}
	}
	// Case-distinct real trades remain distinct even under MySQL's default
	// case-insensitive string collation; the canonical receipt key is a hash.
	authority.Provider, fact.ProviderTradeNo, fact.ProviderEventID = "test-channel-a", "TRADE-A", "case-distinct"
	if out, err := facts.Record(t.Context(), authority, fact); err != nil || out.Disposition != "wallet_credit" {
		t.Fatalf("case-distinct cash identity: %+v %v", out, err)
	}
	// The same actual cash cannot fund another independent order.
	other := f.create(t, 0)
	prepareReceiptAttempt(t, f, other.ID, authority.Provider)
	fact.OrderID, fact.ProviderTradeNo, fact.ProviderEventID = other.ID, "trade-a", "same-cash-another-order"
	if _, err := facts.Record(t.Context(), authority, fact); !errors.Is(err, commerce.ErrPaymentFactConflict) {
		t.Fatalf("same receipt moved to another order: %v", err)
	}
	canceled := f.create(t, 0)
	prepareReceiptAttempt(t, f, canceled.ID, authority.Provider)
	if _, err := f.h.services.OrderCancellation.Owned(t.Context(), 1, canceled.ID); err != nil {
		t.Fatal(err)
	}
	fact.OrderID, fact.ProviderTradeNo, fact.ProviderEventID = canceled.ID, "canceled-real-trade", "canceled-late"
	out, err := facts.Record(t.Context(), authority, fact)
	if err != nil || out.Status != "canceled" || out.Fulfilled || out.Disposition != "wallet_credit" {
		t.Fatalf("cancellation swallowed real payment: %+v %v", out, err)
	}
	if err := datastore.RunMigrations(f.h.db); err != nil {
		t.Fatal(err)
	}
	wallet, err = f.h.services.Wallet.Read(t.Context(), 1, "CNY", 0, 50)
	if err != nil || wallet.BalanceMinor != 3*order.PayableAmount || len(wallet.Entries) != 3 {
		t.Fatalf("receipt replay/migration changed balance: %+v %v", wallet, err)
	}
}

func TestPaymentReceiptPreservesLateRenewalCashWithoutRevival(t *testing.T) {
	assertLateRenewalReceipt(t, newOrderFixture(t))
}
func TestMySQLPaymentReceiptPreservesLateRenewalCashWithoutRevival(t *testing.T) {
	assertLateRenewalReceipt(t, newMySQLOrderFixture(t))
}
func assertLateRenewalReceipt(t *testing.T, f orderFixture) {
	t.Helper()
	paid := f.paid(t, f.create(t, 0).ID)
	renewal := f.create(t, paid.SubscriptionID)
	prepareReceiptAttempt(t, f, renewal.ID, "late-renewal-channel")
	oldEnd := time.Now().UTC().Add(-8 * 24 * time.Hour)
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Update("end_at", oldEnd).Error; err != nil {
		t.Fatal(err)
	}
	fact := commerce.PaymentFact{OrderID: renewal.ID, ProviderEventID: "late-renewal", ProviderTradeNo: "late-renewal-trade", Status: "paid", AmountMinor: renewal.PayableAmount, Currency: renewal.Currency, OccurredAt: time.Now().UTC()}
	facts := receiptService(f)
	authority := commerce.PaymentFactAuthority{AccountID: 1, Provider: "late-renewal-channel", RequireAttempt: true}
	out, err := facts.Record(t.Context(), authority, fact)
	if err != nil || out.Fulfilled || out.Disposition != "wallet_credit" || out.Status != "canceled" {
		t.Fatalf("late renewal: %+v %v", out, err)
	}
	fact.ProviderEventID = "late-renewal-retry"
	if _, err := facts.Record(t.Context(), authority, fact); err != nil {
		t.Fatal(err)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil || sub.EndAt.After(time.Now()) {
		t.Fatalf("late cash revived ended subscription: %+v %v", sub, err)
	}
	wallet, err := f.h.services.Wallet.Read(t.Context(), 1, "CNY", 0, 50)
	if err != nil || wallet.BalanceMinor != renewal.PayableAmount || len(wallet.Entries) != 1 {
		t.Fatalf("late renewal money: %+v %v", wallet, err)
	}
}

func TestPaymentReceiptManualHistoryRequiresTicketedResolution(t *testing.T) {
	assertReceiptResolution(t, newOrderFixture(t), "paid")
}
func TestMySQLPaymentReceiptManualHistoryRequiresTicketedResolution(t *testing.T) {
	assertReceiptResolution(t, newMySQLOrderFixture(t), "paid")
}
func TestPaymentReceiptLegacySuccessHistoryRequiresReview(t *testing.T) {
	assertReceiptResolution(t, newOrderFixture(t), "success")
}
func TestMySQLPaymentReceiptLegacySuccessHistoryRequiresReview(t *testing.T) {
	assertReceiptResolution(t, newMySQLOrderFixture(t), "success")
}
func assertReceiptResolution(t *testing.T, f orderFixture, status string) {
	t.Helper()
	order := f.paid(t, f.create(t, 0).ID)
	if status != "paid" {
		if err := f.h.db.Model(&order).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
	}

	fact := commerce.PaymentFact{OrderID: order.ID, ProviderEventID: "legacy-unknown", ProviderTradeNo: "legacy-actual-trade", Status: "paid", AmountMinor: order.PayableAmount, Currency: order.Currency, OccurredAt: time.Now().UTC()}
	facts := receiptService(f)
	authority := commerce.PaymentFactAuthority{AccountID: 1, Provider: "integration:review", RequireAdministrator: true}
	out, err := facts.Record(t.Context(), authority, fact)
	if err != nil || out.Disposition != "review_required" || out.Fulfilled {
		t.Fatalf("manual history fabricated receipt ownership: %+v %v", out, err)
	}
	page, err := f.h.services.PaymentReceipts.Read(t.Context(), 1, order.ID, true, 0, 25)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("receipt visibility: %+v %v", page, err)
	}
	receipt := page.Items[0]
	if receipt.Provider != authority.Provider || receipt.ProviderTradeNo != fact.ProviderTradeNo {
		t.Fatal("administrator cannot reconcile the actual channel trade")
	}

	read := httptest.NewRecorder()
	f.h.PaymentReceiptsHandler(read, announcementRequest("GET", fmt.Sprintf("/api/v1/orders/%d/payment-receipts", order.ID), f.token, ""))
	if read.Code != 200 || strings.Contains(read.Body.String(), fact.ProviderTradeNo) || strings.Contains(read.Body.String(), authority.Provider) || !strings.Contains(read.Body.String(), `"amount_minor":"100"`) {
		t.Fatalf("unsafe receipt summary: %d %s", read.Code, read.Body.String())
	}
	outsider := model.User{Email: "receipt-outsider@example.test", Password: "unused", Status: "active"}
	if err := f.h.db.Create(&outsider).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.PaymentReceipts.Read(t.Context(), outsider.ID, order.ID, false, 0, 25); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("foreign owner read receipt: %v", err)
	}
	wrongTicket := model.Ticket{TicketNo: "wrong-owner-ticket", UserID: outsider.ID, Subject: "other owner", Category: "billing", Status: "open", LastMessageAt: time.Now().UTC()}
	if err := f.h.db.Create(&wrongTicket).Error; err != nil {
		t.Fatal(err)
	}
	wrong := commerce.ResolveReceiptRequest{ReceiptID: receipt.ID, TicketID: wrongTicket.ID, Decision: "credit_wallet", Reason: "different owner"}
	var invalid *commerce.ValidationError
	if _, err := f.h.services.PaymentReceipts.Resolve(t.Context(), 1, order.ID, wrong); !errors.As(err, &invalid) {
		t.Fatalf("foreign ticket authorized funds: %v", err)
	}
	ticket := model.Ticket{TicketNo: "receipt-review-ticket", UserID: 1, Subject: "payment reconciliation", Category: "billing", Status: "open", LastMessageAt: time.Now().UTC()}
	if err := f.h.db.Create(&ticket).Error; err != nil {
		t.Fatal(err)
	}
	request := commerce.ResolveReceiptRequest{ReceiptID: receipt.ID, TicketID: ticket.ID, Decision: "credit_wallet", Reason: "核对渠道账单及旧人工记录，确认此笔尚未入账。"}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.PaymentReceipts.Resolve(t.Context(), 1, order.ID, request); !errors.Is(err, commerce.ErrOrderPermission) {
		t.Fatalf("ordinary user resolved funds: %v", err)
	}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		result, err := f.h.services.PaymentReceipts.Resolve(t.Context(), 1, order.ID, request)
		if err != nil || result.Disposition != "wallet_credit" || result.WalletEntryID == nil || result.TicketID == nil {
			t.Fatalf("ticketed receipt resolve: %+v %v", result, err)
		}
	}
	request.Decision = "already_applied"
	if _, err := f.h.services.PaymentReceipts.Resolve(t.Context(), 1, order.ID, request); !errors.Is(err, commerce.ErrPaymentFactConflict) {
		t.Fatalf("repeated receipt changed decision: %v", err)
	}
	wallet, err := f.h.services.Wallet.Read(t.Context(), 1, "CNY", 0, 50)
	if err != nil || wallet.BalanceMinor != order.PayableAmount || len(wallet.Entries) != 1 {
		t.Fatalf("review credited twice: %+v %v", wallet, err)
	}
	owner, err := f.h.services.PaymentReceipts.Read(t.Context(), 1, order.ID, false, 0, 25)
	if err != nil || owner.Items[0].ResolutionReason != "" || owner.Items[0].TicketID != nil || owner.Items[0].Provider != "" || owner.Items[0].ProviderTradeNo != "" {
		t.Fatalf("private review notes leaked: %+v %v", owner, err)
	}
}

func TestPaymentReceiptMigrationProtectsRecordedCash(t *testing.T) {
	assertReceiptSchema(t, newOrderFixture(t))
}
func TestMySQLPaymentReceiptMigrationProtectsRecordedCash(t *testing.T) {
	assertReceiptSchema(t, newMySQLOrderFixture(t))
}
func assertReceiptSchema(t *testing.T, f orderFixture) {
	t.Helper()
	original := f.create(t, 0)
	if err := f.h.db.Exec("DELETE FROM schema_migrations WHERE version = ?", "0039_payment_receipts.up.sql").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Exec("DROP TABLE payment_receipts").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := datastore.RunMigrations(f.h.db); err != nil {
			t.Fatal(err)
		}
	}
	var retained model.Order
	if err := f.h.db.First(&retained, original.ID).Error; err != nil || retained.Status != "pending" || retained.AmountCents != original.AmountCents {
		t.Fatalf("receipt migration rewrote order: %+v %v", retained, err)
	}
	// A recorded loss must fail before SQLite AutoMigrate can invent an empty
	// receipt journal; retaining the attempt table does not prove no cash existed.
	if err := f.h.db.Exec("DROP TABLE payment_receipts").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err == nil || f.h.db.Migrator().HasTable("payment_receipts") {
		t.Fatalf("lost cash journal was recreated: %v", err)
	}
}
func TestPaymentReceiptMigrationRefusesLostFinancialColumn(t *testing.T) {
	assertReceiptLostColumn(t, newOrderFixture(t))
}
func TestMySQLPaymentReceiptMigrationRefusesLostFinancialColumn(t *testing.T) {
	assertReceiptLostColumn(t, newMySQLOrderFixture(t))
}
func assertReceiptLostColumn(t *testing.T, f orderFixture) {
	t.Helper()
	if err := f.h.db.Migrator().DropColumn(&model.PaymentReceipt{}, "Disposition"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err == nil || f.h.db.Migrator().HasColumn(&model.PaymentReceipt{}, "Disposition") {
		t.Fatalf("lost cash disposition was fabricated: %v", err)
	}
}
