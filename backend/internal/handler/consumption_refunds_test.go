package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func refundPaidOrder(t testing.TB, f orderFixture, wallet int64) model.Order {
	t.Helper()
	if err := f.h.db.Model(&f.planRecord).Updates(map[string]any{"traffic_bytes": 100, "reset_policy": 2}).Error; err != nil {
		t.Fatal(err)
	}
	priceForFunding(t, f, 1000)
	if wallet > 0 {
		fundTestWallet(t, f, wallet)
	}
	order := f.create(t, 0)
	funding, err := fundingService(f).Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: wallet > 0})
	if err != nil {
		t.Fatal(err)
	}
	if funding.CashMinor == 0 {
		if _, err := fundingService(f).Pay(t.Context(), 1, order.ID); err != nil {
			t.Fatal(err)
		}
	} else {
		provider := "refund-source-channel"
		prepareReceiptAttempt(t, f, order.ID, provider)
		if _, err := receiptService(f).Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: provider, RequireAttempt: true}, commerce.PaymentFact{OrderID: order.ID, ProviderEventID: "refund-source-event", ProviderTradeNo: "refund-source-trade", Status: "paid", AmountMinor: funding.CashMinor, Currency: "CNY", OccurredAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.h.db.First(&order, order.ID).Error; err != nil {
		t.Fatal(err)
	}
	return order
}

func refundCloseOrder(t testing.TB, f orderFixture, order model.Order) {
	t.Helper()
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", order.SubscriptionID).Update("flow_used", 80).Error; err != nil {
		t.Fatal(err)
	}
	quote, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, order.SubscriptionID)
	if err != nil || quote.TotalMinor != 200 {
		t.Fatalf("close quote: %+v %v", quote, err)
	}
	if _, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, order.SubscriptionID, commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "close-before-approved-refund"}); err != nil {
		t.Fatal(err)
	}
}

func refundRequest(t testing.TB, f orderFixture, key string, amount int64) commerce.ConsumptionRefundRequest {
	t.Helper()
	return commerce.ConsumptionRefundRequest{IdempotencyKey: key, AmountMinor: amount, TicketID: adjustmentTicket(t, f).ID, Reason: "核准原订单消费退款"}
}

func TestConsumptionRefundFundingSourcesAndReplay(t *testing.T) {
	for _, wallet := range []int64{0, 300, 1000} {
		t.Run(fmt.Sprint(wallet), func(t *testing.T) { assertConsumptionRefundSources(t, newOrderFixture(t), wallet) })
	}
}
func TestMySQLConsumptionRefundFundingSourcesAndReplay(t *testing.T) {
	for _, wallet := range []int64{0, 300, 1000} {
		t.Run(fmt.Sprint(wallet), func(t *testing.T) { assertConsumptionRefundSources(t, newMySQLOrderFixture(t), wallet) })
	}
}
func assertConsumptionRefundSources(t *testing.T, f orderFixture, wallet int64) {
	order := refundPaidOrder(t, f, wallet)
	refundCloseOrder(t, f, order)
	var sub model.Subscription
	if err := f.h.db.First(&sub, order.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(sub)
	s := f.h.services.ConsumptionRefunds
	in := refundRequest(t, f, "approved-refund-first", 300)
	first, err := s.Approve(t.Context(), 1, order.ID, in)
	if err != nil || first.AmountMinor != 300 || first.Currency != "CNY" || first.TicketID != in.TicketID || first.ActorID != 1 {
		t.Fatalf("approved refund: %+v %v", first, err)
	}
	for range 3 {
		replayed, err := s.Approve(t.Context(), 1, order.ID, in)
		if err != nil || replayed.ID != first.ID || replayed.EntryID != first.EntryID {
			t.Fatalf("replay: %+v %v", replayed, err)
		}
	}
	changed := in
	changed.AmountMinor++
	if _, err := s.Approve(t.Context(), 1, order.ID, changed); !errors.Is(err, commerce.ErrConsumptionRefundConflict) {
		t.Fatalf("changed replay: %v", err)
	}
	in.IdempotencyKey, in.AmountMinor = "approved-refund-second", 500
	if _, err := s.Approve(t.Context(), 1, order.ID, in); err != nil {
		t.Fatal(err)
	}
	in.IdempotencyKey, in.AmountMinor = "approved-refund-too-much", 1
	if _, err := s.Approve(t.Context(), 1, order.ID, in); !errors.Is(err, commerce.ErrSubscriptionSettlementConflict) {
		t.Fatalf("reused original value: %v", err)
	}
	var current model.Order
	var budget model.OrderValueBudget
	var account model.WalletAccount
	var entry model.WalletEntry
	if err := f.h.db.First(&current, order.ID).Error; err != nil || current.PaidAmount != 1000 || current.RefundAmount != 1000 || current.RefundedAt == nil {
		t.Fatalf("original money rewritten: %+v %v", current, err)
	}
	if err := f.h.db.Where("order_id = ?", order.ID).First(&budget).Error; err != nil || budget.FundingMinor != 1000 || budget.RefundedMinor != 1000 {
		t.Fatalf("source budget: %+v %v", budget, err)
	}
	if err := f.h.db.Where("user_id = 1 AND currency = ?", "CNY").First(&account).Error; err != nil || account.BalanceMinor != 1000 || account.ReservedMinor != 0 {
		t.Fatalf("refund not uniformly credited to wallet: %+v %v", account, err)
	}
	if err := f.h.db.First(&entry, first.EntryID).Error; err != nil || entry.Kind != "consumption_refund" || entry.BalanceDelta != 300 || entry.ReservedDelta != 0 || entry.SourceOrderID == nil || *entry.SourceOrderID != order.ID {
		t.Fatalf("lost original funding identity: %+v %v", entry, err)
	}
	if err := f.h.db.First(&sub, order.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(sub)
	if string(before) != string(after) {
		t.Fatal("refund modified settled subscription rights")
	}
	read, err := s.Read(t.Context(), 1, first.ID)
	if err != nil || read.ID != first.ID || read.Reason != first.Reason {
		t.Fatalf("refund read: %+v %v", read, err)
	}
	var audits int64
	if err := f.h.db.Model(&model.AuditLog{}).Where("action = ?", "order.consumption_refund").Count(&audits).Error; err != nil || audits != 2 {
		t.Fatalf("approval audit missing or replayed: %d %v", audits, err)
	}
}

func TestConsumptionRefundRetainsOtherOrderReservations(t *testing.T) {
	assertConsumptionRefundReservations(t, newOrderFixture(t))
}
func TestMySQLConsumptionRefundRetainsOtherOrderReservations(t *testing.T) {
	assertConsumptionRefundReservations(t, newMySQLOrderFixture(t))
}
func assertConsumptionRefundReservations(t *testing.T, f orderFixture) {
	order := refundPaidOrder(t, f, 1000)
	refundCloseOrder(t, f, order)
	fundTestWallet(t, f, 500)
	pending := f.create(t, 0)
	if _, err := fundingService(f).Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	in := refundRequest(t, f, "refund-preserves-reservations", 300)
	if _, err := f.h.services.ConsumptionRefunds.Approve(t.Context(), 1, order.ID, in); err != nil {
		t.Fatal(err)
	}
	var account model.WalletAccount
	if err := f.h.db.Where("user_id = 1 AND currency = ?", "CNY").First(&account).Error; err != nil || account.BalanceMinor != 1000 || account.ReservedMinor != 700 {
		t.Fatalf("refund altered existing reservations: %+v %v", account, err)
	}
	var funding model.OrderFunding
	if err := f.h.db.First(&funding, pending.ID).Error; err != nil || funding.State != "reserved" || funding.WalletMinor != 700 {
		t.Fatalf("other checkout changed: %+v %v", funding, err)
	}
}

func TestConsumptionRefundRejectsActiveAndUnknownSources(t *testing.T) {
	assertConsumptionRefundRejections(t, newOrderFixture(t))
}
func TestMySQLConsumptionRefundRejectsActiveAndUnknownSources(t *testing.T) {
	assertConsumptionRefundRejections(t, newMySQLOrderFixture(t))
}
func assertConsumptionRefundRejections(t *testing.T, f orderFixture) {
	order := refundPaidOrder(t, f, 0)
	in := refundRequest(t, f, "refund-active-entitlement", 100)
	if _, err := f.h.services.ConsumptionRefunds.Approve(t.Context(), 1, order.ID, in); !errors.Is(err, commerce.ErrConsumptionRefundReview) {
		t.Fatalf("active partial refund invented entitlement policy: %v", err)
	}
	refundCloseOrder(t, f, order)
	other := model.User{Email: "refund-other@example.test", Password: "unused", Status: "active"}
	if err := f.h.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.ConsumptionRefunds.Approve(t.Context(), other.ID, order.ID, in); !errors.Is(err, commerce.ErrOrderPermission) {
		t.Fatalf("ordinary account approved refund: %v", err)
	}
	ticket := model.Ticket{TicketNo: "other-refund-ticket", UserID: other.ID, Subject: "other", LastMessageAt: time.Now().UTC()}
	if err := f.h.db.Create(&ticket).Error; err != nil {
		t.Fatal(err)
	}
	in.TicketID = ticket.ID
	if _, err := f.h.services.ConsumptionRefunds.Approve(t.Context(), 1, order.ID, in); err == nil {
		t.Fatal("cross-account ticket authorized refund")
	}
	in.TicketID = 1
	if err := f.h.db.Model(&model.Order{}).Where("id = ?", order.ID).Update("entitlement_value_snapshot", "{}").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.ConsumptionRefunds.Approve(t.Context(), 1, order.ID, in); !errors.Is(err, commerce.ErrSubscriptionValueUnavailable) {
		t.Fatalf("unknown actual payment became refund: %v", err)
	}
	var current model.Order
	if err := f.h.db.First(&current, order.ID).Error; err != nil || current.RefundAmount != 200 || current.PaidAmount != 1000 {
		t.Fatalf("rejected refund changed source: %+v %v", current, err)
	}
}

func TestConsumptionRefundFailureRollsBackAllFacts(t *testing.T) {
	for _, target := range []string{"consumption_refunds", "audit_logs", "wallet_entries"} {
		t.Run(target, func(t *testing.T) { assertConsumptionRefundRollback(t, newOrderFixture(t), target) })
	}
}
func TestMySQLConsumptionRefundFailureRollsBackAllFacts(t *testing.T) {
	for _, target := range []string{"consumption_refunds", "audit_logs", "wallet_entries"} {
		t.Run(target, func(t *testing.T) { assertConsumptionRefundRollback(t, newMySQLOrderFixture(t), target) })
	}
}
func assertConsumptionRefundRollback(t *testing.T, f orderFixture, target string) {
	order := refundPaidOrder(t, f, 0)
	refundCloseOrder(t, f, order)
	in := refundRequest(t, f, "rollback-approved-refund", 300)
	sql := fmt.Sprintf("CREATE TRIGGER reject_consumption_refund BEFORE INSERT ON %s BEGIN SELECT RAISE(ABORT, 'refund unavailable'); END", target)
	if !datastore.IsSQLite(f.h.db) {
		sql = fmt.Sprintf("CREATE TRIGGER reject_consumption_refund BEFORE INSERT ON %s FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'refund unavailable'", target)
	}
	if err := f.h.db.Exec(sql).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.ConsumptionRefunds.Approve(t.Context(), 1, order.ID, in); err == nil {
		t.Fatal("failed refund acknowledged")
	}
	var current model.Order
	var budget model.OrderValueBudget
	var account model.WalletAccount
	var refunds int64
	if err := f.h.db.First(&current, order.ID).Error; err != nil || current.RefundAmount != 200 {
		t.Fatal("source partially refunded", current.RefundAmount, err)
	}
	if err := f.h.db.First(&budget, order.ID).Error; err != nil || budget.RefundedMinor != 200 {
		t.Fatal("budget partially consumed", budget.RefundedMinor, err)
	}
	if err := f.h.db.Where("user_id = 1 AND currency = ?", "CNY").First(&account).Error; err != nil || account.BalanceMinor != 200 {
		t.Fatal("wallet partially credited", account.BalanceMinor, err)
	}
	if err := f.h.db.Model(&model.ConsumptionRefund{}).Count(&refunds).Error; err != nil || refunds != 0 {
		t.Fatal("approval partly committed", refunds, err)
	}
	if err := f.h.db.Exec("DROP TRIGGER reject_consumption_refund").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.ConsumptionRefunds.Approve(t.Context(), 1, order.ID, in); err != nil {
		t.Fatal("rollback consumed idempotency identity", err)
	}
}

func TestConsumptionRefundConcurrentReplayAndBudget(t *testing.T) {
	assertConsumptionRefundConcurrency(t, newOrderFixture(t))
}
func TestMySQLConsumptionRefundConcurrentReplayAndBudget(t *testing.T) {
	assertConsumptionRefundConcurrency(t, newMySQLOrderFixture(t))
}
func assertConsumptionRefundConcurrency(t *testing.T, f orderFixture) {
	order := refundPaidOrder(t, f, 0)
	refundCloseOrder(t, f, order)
	in := refundRequest(t, f, "concurrent-approved-refund", 300)
	var wg sync.WaitGroup
	results := make(chan error, 6)
	for range 6 {
		wg.Go(func() {
			_, err := f.h.services.ConsumptionRefunds.Approve(t.Context(), 1, order.ID, in)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal("identical approval did not replay", err)
		}
	}
	results = make(chan error, 2)
	for i := range 2 {
		wg.Go(func() {
			request := in
			request.IdempotencyKey = fmt.Sprintf("competing-approved-refund-%d", i)
			request.AmountMinor = 400
			_, err := f.h.services.ConsumptionRefunds.Approve(t.Context(), 1, order.ID, request)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, commerce.ErrSubscriptionSettlementConflict) {
			t.Fatal("unexpected competing approval failure", err)
		}
	}
	var current model.Order
	if err := f.h.db.First(&current, order.ID).Error; err != nil || current.RefundAmount != 900 || success != 1 {
		t.Fatalf("concurrent approval exceeded source: refund=%d successes=%d err=%v", current.RefundAmount, success, err)
	}
}

func TestConsumptionRefundHTTPStrictApprovalBoundary(t *testing.T) {
	f := newOrderFixture(t)
	order := refundPaidOrder(t, f, 0)
	in := refundRequest(t, f, "http-approved-refund", 100)
	path := fmt.Sprintf("/api/v1/admin/orders/%d/consumption-refunds", order.ID)
	body, _ := json.Marshal(in)
	out := httptest.NewRecorder()
	f.h.AdminOrderConsumptionRefundsHandler(out, announcementRequest(http.MethodPost, path, f.token, string(body)))
	if out.Code != http.StatusConflict || !strings.Contains(out.Body.String(), "权益") {
		t.Fatalf("active refund explanation: %d %s", out.Code, out.Body.String())
	}
	refundCloseOrder(t, f, order)
	for _, raw := range []string{strings.TrimSuffix(string(body), "}") + `,"status":"paid"}`, strings.TrimSuffix(string(body), "}") + `,"paid_amount":1000}`} {
		out = httptest.NewRecorder()
		f.h.AdminOrderConsumptionRefundsHandler(out, announcementRequest(http.MethodPost, path, f.token, raw))
		if out.Code != http.StatusBadRequest {
			t.Fatalf("refund accepted payment authority fields: %d %s", out.Code, out.Body.String())
		}
	}
	out = httptest.NewRecorder()
	f.h.AdminOrderConsumptionRefundsHandler(out, announcementRequest(http.MethodPost, path, "", string(body)))
	if out.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated approval: %d", out.Code)
	}
	out = httptest.NewRecorder()
	f.h.AdminOrderConsumptionRefundsHandler(out, announcementRequest(http.MethodPost, path, f.token, string(body)))
	if out.Code != http.StatusOK || out.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("HTTP approval: %d %s", out.Code, out.Body.String())
	}
}

func TestConsumptionRefundOldReplacementLeavesNewRightsUnchanged(t *testing.T) {
	assertConsumptionRefundOldReplacement(t, newOrderFixture(t))
}
func TestMySQLConsumptionRefundOldReplacementLeavesNewRightsUnchanged(t *testing.T) {
	assertConsumptionRefundOldReplacement(t, newMySQLOrderFixture(t))
}
func assertConsumptionRefundOldReplacement(t *testing.T, base orderFixture) {
	f := newFullReplacementFixture(t, base, false, 0, 3)
	order := confirmedReplacement(t, f, "replacement-before-approved-refund")
	if _, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Pay(t.Context(), 1, order.ID); err != nil {
		t.Fatal(err)
	}
	var before, after model.Subscription
	if err := f.db.First(&before, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	in := refundRequest(t, base, "old-replacement-approved-refund", 3000)
	if _, err := base.h.services.ConsumptionRefunds.Approve(t.Context(), 1, f.root.ID, in); err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&after, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	one, _ := json.Marshal(before)
	two, _ := json.Marshal(after)
	if string(one) != string(two) || after.EntitlementEpoch != 1 || after.Status != "active" {
		t.Fatal("old consumption refund changed the replacement term")
	}
	var current model.Order
	if err := f.db.First(&current, order.ID).Error; err != nil || current.PaidAmount != 2000 || current.RefundAmount != 0 || current.SubscriptionEndedAt != nil {
		t.Fatal("old refund changed new order value", current, err)
	}
	in.IdempotencyKey, in.AmountMinor = "replacement-residual-already-returned", 1
	if _, err := base.h.services.ConsumptionRefunds.Approve(t.Context(), 1, f.root.ID, in); !errors.Is(err, commerce.ErrSubscriptionSettlementConflict) {
		t.Fatal("replacement residual returned twice", err)
	}
}

func TestConsumptionRefundProtectsConvertedAndUnprovenBudgets(t *testing.T) {
	assertConsumptionRefundBudgetProtection(t, newOrderFixture(t))
}
func TestMySQLConsumptionRefundProtectsConvertedAndUnprovenBudgets(t *testing.T) {
	assertConsumptionRefundBudgetProtection(t, newMySQLOrderFixture(t))
}
func assertConsumptionRefundBudgetProtection(t *testing.T, f orderFixture) {
	order := refundPaidOrder(t, f, 0)
	refundCloseOrder(t, f, order)
	if err := f.h.db.Model(&model.OrderValueBudget{}).Where("order_id = ?", order.ID).Update("converted_minor", 700).Error; err != nil {
		t.Fatal(err)
	}
	in := refundRequest(t, f, "converted-source-protection", 101)
	if _, err := f.h.services.ConsumptionRefunds.Approve(t.Context(), 1, order.ID, in); !errors.Is(err, commerce.ErrSubscriptionSettlementConflict) {
		t.Fatal("converted source was refunded again", err)
	}
	in.AmountMinor = 100
	if _, err := f.h.services.ConsumptionRefunds.Approve(t.Context(), 1, order.ID, in); err != nil {
		t.Fatal("unconverted remainder was not refundable", err)
	}
	if err := f.h.db.Model(&model.OrderValueBudget{}).Where("order_id = ?", order.ID).Update("funding_fingerprint", strings.Repeat("a", 64)).Error; err != nil {
		t.Fatal(err)
	}
	in.IdempotencyKey, in.AmountMinor = "unproven-source-protection", 1
	if _, err := f.h.services.ConsumptionRefunds.Approve(t.Context(), 1, order.ID, in); !errors.Is(err, commerce.ErrSubscriptionSettlementConflict) {
		t.Fatal("unproven source was replaced with invented funding", err)
	}
	topup := createTopUp(t, f, 100, "refund-must-not-recharge")
	if _, err := f.h.services.ConsumptionRefunds.Approve(t.Context(), 1, topup.ID, in); err == nil {
		t.Fatal("top-up became another wallet credit")
	}
}

func TestConsumptionRefundRacesUnsubscribeWithoutReturningValueTwice(t *testing.T) {
	assertConsumptionRefundUnsubscribeRace(t, newOrderFixture(t))
}
func TestMySQLConsumptionRefundRacesUnsubscribeWithoutReturningValueTwice(t *testing.T) {
	assertConsumptionRefundUnsubscribeRace(t, newMySQLOrderFixture(t))
}
func assertConsumptionRefundUnsubscribeRace(t *testing.T, f orderFixture) {
	order := refundPaidOrder(t, f, 0)
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", order.SubscriptionID).Update("flow_used", 80).Error; err != nil {
		t.Fatal(err)
	}
	quote, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, order.SubscriptionID)
	if err != nil || quote.TotalMinor != 200 {
		t.Fatal("race quote", quote, err)
	}
	in := refundRequest(t, f, "refund-racing-unsubscribe", 800)
	termination := commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "unsubscribe-racing-refund"}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var refundErr, terminationErr error
	wg.Go(func() {
		<-start
		_, refundErr = f.h.services.ConsumptionRefunds.Approve(t.Context(), 1, order.ID, in)
	})
	wg.Go(func() {
		<-start
		_, terminationErr = f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, order.SubscriptionID, termination)
	})
	close(start)
	wg.Wait()
	if terminationErr != nil || (refundErr != nil && !errors.Is(refundErr, commerce.ErrConsumptionRefundReview)) {
		t.Fatal("unexpected financial race outcome", terminationErr, refundErr)
	}
	// A refund rejected while rights were active can be explicitly retried only
	// after closure. A successful approval replays the exact same wallet fact.
	if _, err := f.h.services.ConsumptionRefunds.Approve(t.Context(), 1, order.ID, in); err != nil {
		t.Fatal("post-close approval/replay", err)
	}
	replay, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, order.SubscriptionID, termination)
	if err != nil || replay.AmountMinor != 200 {
		t.Fatal("old settlement changed after independent refund", replay, err)
	}
	var current model.Order
	var account model.WalletAccount
	var count int64
	if err := f.h.db.First(&current, order.ID).Error; err != nil || current.PaidAmount != 1000 || current.RefundAmount != 1000 {
		t.Fatal("racing settlements duplicated source value", current, err)
	}
	if err := f.h.db.Where("user_id = 1 AND currency = ?", "CNY").First(&account).Error; err != nil || account.BalanceMinor != 1000 {
		t.Fatal("racing settlements duplicated wallet cash", account, err)
	}
	if err := f.h.db.Model(&model.ConsumptionRefund{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("racing approval lost idempotency identity", count, err)
	}
}
