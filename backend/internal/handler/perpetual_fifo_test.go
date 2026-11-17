package handler

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestPerpetualFIFOFundedPurchasesMeteringAndWholeUnsubscribe(t *testing.T) {
	assertPerpetualFIFOFundedPurchases(t, newOrderFixture(t), false)
}
func TestMySQLPerpetualFIFOFundedPurchasesMeteringAndWholeUnsubscribe(t *testing.T) {
	assertPerpetualFIFOFundedPurchases(t, newMySQLBusinessOrderFixture(t), false)
}
func TestPerpetualFIFOFundedPurchasesFullReplacement(t *testing.T) {
	assertPerpetualFIFOFundedPurchases(t, newOrderFixture(t), true)
}
func TestMySQLPerpetualFIFOFundedPurchasesFullReplacement(t *testing.T) {
	assertPerpetualFIFOFundedPurchases(t, newMySQLBusinessOrderFixture(t), true)
}
func assertPerpetualFIFOFundedPurchases(t *testing.T, base orderFixture, replacement bool) {
	t.Helper()
	f := base
	attachOrderPublishEndpoint(t, f)
	if err := f.h.db.Model(&f.planRecord).Updates(map[string]any{"traffic_bytes": 100, "reset_policy": 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&f.skuRecord).Updates(map[string]any{"price_cents": 10000, "billing_unit": "once", "billing_value": 1, "billing_mode": "one_time", "renewal_effect": "add_quota_only"}).Error; err != nil {
		t.Fatal(err)
	}
	fundTestWallet(t, f, 30000)
	root := f.create(t, 0)
	if _, err := fundingService(f).Configure(t.Context(), 1, root.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := fundingService(f).Pay(t.Context(), 1, root.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&root, root.ID).Error; err != nil {
		t.Fatal(err)
	}
	completePerpetualFIFOUsage(t, f, root.SubscriptionID, "fifo-first-use", 20)
	oldSnapshot := root.EntitlementValueSnapshot
	oldQuote, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, root.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	// A second real purchase grants 50 quota for 6000 actual: its price differs
	// from the first lot and from the locked 10000 base, so averaging is observable.
	if err := f.h.db.Model(&f.planRecord).Update("traffic_bytes", 50).Error; err != nil {
		t.Fatal(err)
	}
	sku := f.sku(t, f.planRecord.ID, 10000, "renew")
	if err := f.h.db.Model(&sku).Updates(map[string]any{"billing_unit": "once", "billing_value": 1, "billing_mode": "one_time", "renewal_effect": "add_quota_only"}).Error; err != nil {
		t.Fatal(err)
	}
	actual := int64(6000)
	added, err := f.h.services.OrderAssignment.Assign(t.Context(), 1, commerce.OrderAssignmentRequest{UserID: 1, PlanSKUID: sku.ID, TargetSubscriptionID: root.SubscriptionID, PayableAmount: &actual, RequestID: uuid.NewString(), Note: "FIFO real discounted quota purchase"})
	if err != nil {
		t.Fatal(err)
	}
	payments := fundingService(f)
	if _, err := payments.Configure(t.Context(), 1, added.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := payments.Pay(t.Context(), 1, added.ID); err != nil {
			t.Fatal(err)
		}
	}
	var second model.Order
	if err := f.h.db.First(&second, added.ID).Error; err != nil {
		t.Fatal(err)
	}
	var fact commerce.EntitlementValueSnapshot
	if err := json.Unmarshal([]byte(second.EntitlementValueSnapshot), &fact); err != nil || fact.Mode != "perpetual_quota_addition" || fact.AllocationModel != commerce.PerpetualQuotaFIFOAllocation || !fact.PaymentVerified || fact.ActualPaidMinor != 6000 || fact.Before == nil || fact.Before.FlowUsed != 20 || fact.After.FlowTotal-fact.Before.FlowTotal != 50 {
		t.Fatalf("additional purchase evidence %+v %v", fact, err)
	}
	quote, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, root.SubscriptionID)
	if err != nil || quote.TotalMinor != 14000 {
		t.Fatalf("FIFO before crossing old lot %+v %v", quote, err)
	}
	// Neither the old quote nor a higher current catalog price may silently change
	// the approved settlement or historical purchase facts.
	if _, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, root.SubscriptionID, commerce.SubscriptionTerminationRequest{QuoteFingerprint: oldQuote.Fingerprint, IdempotencyKey: "fifo-stale-before-purchase"}); !errors.Is(err, commerce.ErrSubscriptionQuoteChanged) {
		t.Fatalf("old purchase quote accepted: %v", err)
	}
	// Abort at the final audit after both source allocations. All money, source
	// budgets, lifecycle and durable delivery intent must roll back together.
	sql := `CREATE TRIGGER reject_fifo_audit BEFORE INSERT ON audit_logs WHEN NEW.action = 'subscription.unsubscribe' BEGIN SELECT RAISE(ABORT, 'fifo audit unavailable'); END`
	if !datastore.IsSQLite(f.h.db) {
		sql = `CREATE TRIGGER reject_fifo_audit BEFORE INSERT ON audit_logs FOR EACH ROW BEGIN IF NEW.action = 'subscription.unsubscribe' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'fifo audit unavailable'; END IF; END`
	}
	if err := f.h.db.Exec(sql).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, root.SubscriptionID, commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "fifo-audit-rollback"}); err == nil {
		t.Fatal("audit failure committed FIFO refund")
	}
	for _, table := range []string{"subscription_value_settlements", "subscription_value_settlement_sources", "order_value_budgets"} {
		var count int64
		if err := f.h.db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("partial FIFO %s %d %v", table, count, err)
		}
	}
	var afterAbort model.WalletAccount
	if err := f.h.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&afterAbort).Error; err != nil || afterAbort.BalanceMinor != 14000 {
		t.Fatalf("partial FIFO wallet %+v %v", afterAbort, err)
	}
	var beforeUse model.Subscription
	if err := f.h.db.First(&beforeUse, root.SubscriptionID).Error; err != nil || beforeUse.Status != "active" || beforeUse.EndedAt != nil {
		t.Fatalf("partial FIFO rights %+v %v", beforeUse, err)
	}
	if err := f.h.db.Exec("DROP TRIGGER reject_fifo_audit").Error; err != nil {
		t.Fatal(err)
	}
	completePerpetualFIFOUsage(t, f, root.SubscriptionID, "fifo-cross-old-lot", 90)
	completePerpetualFIFOUsage(t, f, root.SubscriptionID, "fifo-cross-old-lot", 90) // duplicate event, no duplicate use.
	quote, err = f.h.services.SubscriptionValues.Read(t.Context(), 1, root.SubscriptionID)
	if err != nil || quote.TotalMinor != 4800 || quote.FutureMinor != 0 {
		t.Fatalf("FIFO crossed old lot (weighted average would differ) %+v %v", quote, err)
	}
	if err := f.h.db.Model(&sku).Update("price_cents", 25000).Error; err != nil {
		t.Fatal(err)
	}
	unchanged, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, root.SubscriptionID)
	if err != nil || unchanged.Fingerprint != quote.Fingerprint || unchanged.TotalMinor != 4800 {
		t.Fatalf("catalog repriced purchased quota %+v %v", unchanged, err)
	}
	if replacement {
		assertPerpetualFIFOReplacement(t, f, root, second)
		return
	}
	request := commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "fifo-whole-unsubscribe"}
	var receiptID uint
	for range 3 {
		receipt, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, root.SubscriptionID, request)
		if err != nil || receipt.AmountMinor != 4800 || receipt.Status != "canceled" || receiptID != 0 && receiptID != receipt.ID {
			t.Fatalf("FIFO whole unsubscribe %+v %v", receipt, err)
		}
		receiptID = receipt.ID
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, root.SubscriptionID, request)
			if err == nil && (receipt.ID != receiptID || receipt.AmountMinor != 4800) {
				err = errors.New("FIFO replay changed receipt")
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent FIFO replay: %v", err)
		}
	}
	var wallet model.WalletAccount
	if err := f.h.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 18800 || wallet.ReservedMinor != 0 {
		t.Fatalf("FIFO money conservation %+v %v", wallet, err)
	}
	if err := f.h.db.First(&root, root.ID).Error; err != nil || root.RefundAmount != 0 || root.PaidAmount != 10000 || root.EntitlementValueSnapshot != oldSnapshot || root.SubscriptionEndedAt == nil {
		t.Fatalf("consumed first source rewritten %+v %v", root, err)
	}
	if err := f.h.db.First(&second, second.ID).Error; err != nil || second.RefundAmount != 4800 || second.PaidAmount != 6000 || second.SubscriptionEndedAt == nil {
		t.Fatalf("second lot refund %+v %v", second, err)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil || sub.Status != "canceled" || sub.EndedAt == nil || sub.FlowUsed != 110 {
		t.Fatalf("whole rights termination %+v %v", sub, err)
	}
	var active int64
	if err := f.h.db.Model(&model.ProtocolCredential{}).Where("subscription_id = ? AND status <> ?", sub.ID, "revoked").Count(&active).Error; err != nil || active != 0 {
		t.Fatalf("whole unsubscribe kept credentials %d %v", active, err)
	}
	if _, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, sub.ID, commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "fifo-other-request"}); !errors.Is(err, commerce.ErrSubscriptionSettlementConflict) {
		t.Fatalf("different request returned same value: %v", err)
	}
}

func assertPerpetualFIFOReplacement(t *testing.T, f orderFixture, root, second model.Order) {
	t.Helper()
	plan := f.plan(t, 2)
	if err := f.h.db.Model(&plan).Updates(map[string]any{"traffic_bytes": 100, "reset_policy": 1}).Error; err != nil {
		t.Fatal(err)
	}
	sku := f.sku(t, plan.ID, 2000, "change")
	request := commerce.OrderCreateRequest{PlanSKUID: sku.ID, TargetSubscriptionID: root.SubscriptionID, IdempotencyKey: "fifo-complete-new-term"}
	quote, err := f.h.services.OrderCreation.Preview(t.Context(), 1, request)
	if err != nil || quote.SettlementAmount != 4800 || quote.SurplusAmount != 2800 || quote.AdditionalAmount != 0 {
		t.Fatalf("FIFO replacement quote %+v %v", quote, err)
	}
	request.QuoteFingerprint = quote.QuoteFingerprint
	order, err := f.h.services.OrderCreation.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	payments := fundingService(f)
	if _, err := payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := payments.Pay(t.Context(), 1, order.ID); err != nil {
			t.Fatal(err)
		}
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil || sub.FlowUsed != 110 || sub.CycleStartUsed != 110 || sub.FlowTotal != 210 || sub.PlanID != plan.ID || sub.Status != "active" || sub.EntitlementEpoch != 1 {
		t.Fatalf("FIFO replacement carried old rights %+v %v", sub, err)
	}
	var wallet model.WalletAccount
	if err := f.h.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 16800 || wallet.ReservedMinor != 0 {
		t.Fatalf("FIFO replacement cash %+v %v", wallet, err)
	}
	if err := f.h.db.First(&root, root.ID).Error; err != nil || root.RefundAmount != 0 || root.SubscriptionEndedAt == nil {
		t.Fatalf("FIFO exhausted lot conversion %+v %v", root, err)
	}
	if err := f.h.db.First(&second, second.ID).Error; err != nil || second.RefundAmount != 4800 || second.SubscriptionEndedAt == nil {
		t.Fatalf("FIFO remaining lot conversion %+v %v", second, err)
	}
	value, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, sub.ID)
	if err != nil || value.TotalMinor != 2000 {
		t.Fatalf("FIFO replacement new cash ancestry %+v %v", value, err)
	}
	in := commerce.SubscriptionTerminationRequest{QuoteFingerprint: value.Fingerprint, IdempotencyKey: "fifo-replaced-term-unsubscribe"}
	for range 2 {
		receipt, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, sub.ID, in)
		if err != nil || receipt.AmountMinor != 2000 {
			t.Fatalf("FIFO replacement re-refund %+v %v", receipt, err)
		}
	}
	if err := f.h.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 18800 {
		t.Fatalf("FIFO replacement double credit %+v %v", wallet, err)
	}
}

func completePerpetualFIFOUsage(t *testing.T, f orderFixture, sub uint, id string, used int64) {
	t.Helper()
	var credential model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ? AND current_scope = ?", sub, 1).First(&credential).Error; err != nil {
		t.Fatal(err)
	}
	accounting := metering.CompletionAccounting{Repository: meteringstore.CompletionAccounting{DB: f.h.db, Cipher: f.h.credentialCipher, EnqueuePublications: networkstore.EnqueueSubscriptionPublications}}
	event := metering.CompletedFlow{NodeID: credential.NodeID, CoreInstanceID: "fifo-core", EventID: id, EventType: "flow.completed", Sequence: 1, FlowID: id, PrincipalKey: credential.PrincipalKey, BytesDown: used, OccurredAt: time.Now().UTC()}
	if _, _, err := accounting.Complete(t.Context(), event); err != nil {
		t.Fatal(err)
	}
}

func TestWholeUnsubscribeApprovedRemainingValueExamples(t *testing.T) {
	assertWholeUnsubscribeExamples(t, func(t *testing.T) orderFixture { return newOrderFixture(t) })
}
func TestMySQLWholeUnsubscribeApprovedRemainingValueExamples(t *testing.T) {
	assertWholeUnsubscribeExamples(t, newMySQLBusinessOrderFixture)
}
func assertWholeUnsubscribeExamples(t *testing.T, fixture func(*testing.T) orderFixture) {
	t.Helper()
	for _, item := range []struct {
		name     string
		shared   bool
		used     int64
		months   int
		residual int64
	}{
		{"three_complete_months", false, 0, 3, 9000}, {"third_month_eighty_percent", false, 80, 2, 9200}, {"annual_shared_eighty_percent", true, 80, 2, 2400},
	} {
		t.Run(item.name, func(t *testing.T) {
			f := newFullReplacementFixture(t, fixture(t), item.shared, item.used, item.months)
			clock := func() time.Time { return *f.at }
			values := commerce.SubscriptionValues{Repository: commercestore.SubscriptionValues{DB: f.db, Now: clock}}
			terminate := commerce.SubscriptionTermination{Repository: commercestore.SubscriptionTermination{DB: f.db, Now: clock}}
			quote, err := values.Read(t.Context(), 1, f.subscription)
			if err != nil || quote.TotalMinor != item.residual {
				t.Fatalf("whole unsubscribe quote %+v %v", quote, err)
			}
			request := commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "approved-whole-remaining-value"}
			for range 2 {
				receipt, err := terminate.Terminate(t.Context(), 1, f.subscription, request)
				if err != nil || receipt.AmountMinor != item.residual || receipt.Status != "canceled" {
					t.Fatalf("whole unsubscribe %+v %v", receipt, err)
				}
			}
			var sub model.Subscription
			if err := f.db.First(&sub, f.subscription).Error; err != nil || sub.Status != "canceled" || sub.EndedAt == nil || sub.EndReason != "canceled" || sub.FlowUsed != item.used {
				t.Fatalf("remaining rights retained %+v %v", sub, err)
			}
			var wallet model.WalletAccount
			if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 18000+item.residual {
				t.Fatalf("original full price refunded %+v %v", wallet, err)
			}
			if err := f.db.First(&f.root, f.root.ID).Error; err != nil || f.root.RefundAmount != item.residual || f.root.PaidAmount != 12000 {
				t.Fatalf("original payment overwritten %+v %v", f.root, err)
			}
		})
	}
}
