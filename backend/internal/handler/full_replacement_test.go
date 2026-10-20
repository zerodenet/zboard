package handler

import (
	"fmt"
	"github.com/google/uuid"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type fullReplacementFixture struct {
	base          orderFixture
	db            *gorm.DB
	at            *time.Time
	orders        commerce.OrderCreation
	payments      commerce.WalletPayments
	root          model.Order
	subscription  uint
	oldCredential model.ProtocolCredential
	target        uint
}

func newFullReplacementFixture(t *testing.T, base orderFixture, shared bool, used int64, months int) fullReplacementFixture {
	t.Helper()
	attachOrderPublishEndpoint(t, base)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return at }
	db := base.h.db.Session(&gorm.Session{NowFunc: clock})
	policy := int16(1)
	if shared {
		policy = 5
	}
	if err := db.Model(&base.planRecord).Updates(map[string]any{"traffic_bytes": 100, "reset_policy": policy}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&base.skuRecord).Updates(map[string]any{"price_cents": 12000, "billing_unit": "year", "billing_value": 1, "renewal_effect": "extend_only"}).Error; err != nil {
		t.Fatal(err)
	}
	issuer := zeroadapter.Issuer{Cipher: base.h.credentialCipher, Mieru: base.h.zeroMieruAccess}
	orders := commerce.OrderCreation{Repository: commercestore.OrderCreation{DB: db, Now: clock}}
	payments := commerce.WalletPayments{Repository: commercestore.WalletPayments{DB: db, Issuer: issuer, Now: clock}}
	wallet := commerce.Wallet{Repository: commercestore.Wallet{DB: db}}
	topup, err := wallet.CreateTopUp(t.Context(), 1, commerce.TopUpRequest{AmountMinor: 30000, Currency: "CNY", IdempotencyKey: "full-change-test-funding"})
	if err != nil {
		t.Fatal(err)
	}
	facts := commerce.PaymentFacts{Repository: commercestore.PaymentFacts{DB: db, Issuer: issuer, Now: clock}}
	if _, err := facts.Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: "sandbox-full-change", RequireAdministrator: true}, commerce.PaymentFact{OrderID: topup.ID, ProviderEventID: "full-change-funding", ProviderTradeNo: "full-change-funding", Status: "paid", AmountMinor: 30000, Currency: "CNY", OccurredAt: at}); err != nil {
		t.Fatal(err)
	}
	root, err := orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: base.skuRecord.ID, IdempotencyKey: "full-change-root-purchase"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := payments.Configure(t.Context(), 1, root.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := payments.Pay(t.Context(), 1, root.ID); err != nil {
		t.Fatal(err)
	}
	var paid model.Order
	if err := db.First(&paid, root.ID).Error; err != nil {
		t.Fatal(err)
	}
	var credential model.ProtocolCredential
	if err := db.Where("subscription_id = ? AND current_scope = ?", paid.SubscriptionID, 1).First(&credential).Error; err != nil {
		t.Fatal(err)
	}
	at = time.Date(2026, 1+time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	if !shared {
		if _, err := (entitlementstore.TrafficReset{DB: db, Issuer: issuer, Publish: networkstore.EnqueueSubscriptionPublications}).ResetDue(t.Context(), paid.SubscriptionID, at); err != nil {
			t.Fatal(err)
		}
	}
	if used > 0 {
		accounting := metering.CompletionAccounting{Repository: meteringstore.CompletionAccounting{DB: db, Cipher: base.h.credentialCipher, EnqueuePublications: networkstore.EnqueueSubscriptionPublications}}
		event := metering.CompletedFlow{NodeID: credential.NodeID, CoreInstanceID: "full-change-core", EventID: "full-change-old-usage", EventType: "flow.completed", Sequence: 1, FlowID: "old-subscription-flow", PrincipalKey: credential.PrincipalKey, BytesDown: used, OccurredAt: at}
		if _, _, err := accounting.Complete(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	targetPlan := base.plan(t, 2)
	if err := db.Model(&targetPlan).Updates(map[string]any{"traffic_bytes": 100, "reset_policy": 1}).Error; err != nil {
		t.Fatal(err)
	}
	target := base.sku(t, targetPlan.ID, 2000, "change")
	return fullReplacementFixture{base: base, db: db, at: &at, orders: orders, payments: payments, root: paid, subscription: paid.SubscriptionID, oldCredential: credential, target: target.ID}
}
func TestFullReplacementApprovedResidualExamples(t *testing.T) {
	for _, item := range []struct {
		name              string
		shared            bool
		used              int64
		months            int
		residual, surplus int64
	}{{"three_complete_months", false, 0, 3, 9000, 7000}, {"third_month_eighty_percent", false, 80, 2, 9200, 7200}, {"annual_shared_eighty_percent", true, 80, 2, 2400, 400}} {
		t.Run(item.name, func(t *testing.T) {
			assertFullReplacementExample(t, newOrderFixture(t), item.shared, item.used, item.months, item.residual, item.surplus)
		})
	}
}
func TestMySQLFullReplacementApprovedResidualExamples(t *testing.T) {
	for _, item := range []struct {
		name              string
		shared            bool
		used              int64
		months            int
		residual, surplus int64
	}{{"three_complete_months", false, 0, 3, 9000, 7000}, {"third_month_eighty_percent", false, 80, 2, 9200, 7200}, {"annual_shared_eighty_percent", true, 80, 2, 2400, 400}} {
		t.Run(item.name, func(t *testing.T) {
			assertFullReplacementExample(t, newMySQLOrderFixture(t), item.shared, item.used, item.months, item.residual, item.surplus)
		})
	}
}
func assertFullReplacementExample(t *testing.T, base orderFixture, shared bool, used int64, months int, residual, surplus int64) {
	f := newFullReplacementFixture(t, base, shared, used, months)
	request := commerce.OrderCreateRequest{PlanSKUID: f.target, TargetSubscriptionID: f.subscription, IdempotencyKey: "confirmed-full-replacement"}
	quote, err := f.orders.Preview(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	if quote.ChangeMode != "full_term" || quote.SettlementAmount != residual || quote.CreditAmount != 2000 || quote.SurplusAmount != surplus || quote.AdditionalAmount != 0 || quote.PayableAmount != 2000 || quote.UsedBytes != 0 {
		t.Fatalf("wrong approved quote %+v", quote)
	}
	request.QuoteFingerprint = quote.QuoteFingerprint
	created, err := f.orders.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	var order model.Order
	if err := f.db.First(&order, created.ID).Error; err != nil || order.DiscountAmount != 0 || order.PayableAmount != 2000 {
		t.Fatal("old paid value was disguised as discount", err)
	}
	fund, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true})
	if err != nil {
		t.Fatal(err)
	}
	if fund.EntitlementMinor != 2000 || fund.ReservedMinor != 0 || fund.CashMinor != 0 {
		t.Fatalf("projected credit treated as reserved cash %+v", fund)
	}
	var before model.WalletAccount
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&before).Error; err != nil || before.BalanceMinor != 18000 || before.ReservedMinor != 0 {
		t.Fatal("checkout spent old rights before fulfillment", err)
	}
	for range 2 {
		if _, err := f.payments.Pay(t.Context(), 1, order.ID); err != nil {
			t.Fatal(err)
		}
	}
	var after model.WalletAccount
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&after).Error; err != nil || after.BalanceMinor != 18000+surplus || after.ReservedMinor != 0 {
		t.Fatalf("wrong final balance %+v %v", after, err)
	}
	var sub model.Subscription
	if err := f.db.First(&sub, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	total, nowUsed := entitlements.CycleQuota(entitlements.Subscription(sub))
	if sub.ID != f.subscription || sub.EntitlementEpoch != 1 || !sub.StartAt.Equal(*f.at) || !sub.EndAt.Equal(f.at.AddDate(0, 1, 0)) || total != 100 || nowUsed != 0 {
		t.Fatalf("new service carried old time/quota %+v total=%d used=%d", sub, total, nowUsed)
	}
	if err := f.db.First(&order, order.ID).Error; err != nil || order.PaidAmount != 2000 || order.DiscountAmount != 0 {
		t.Fatal("new actual payment snapshot is wrong", err)
	}
	var old model.Order
	if err := f.db.First(&old, f.root.ID).Error; err != nil || old.PaidAmount != 12000 || old.RefundAmount != residual || old.SubscriptionEndedAt == nil || old.SubscriptionEndReason != "changed" {
		t.Fatal("historical actual payment/refund was rewritten", err)
	}
	var budget model.OrderValueBudget
	if err := f.db.First(&budget, f.root.ID).Error; err != nil || budget.FundingMinor != 12000 || budget.RefundedMinor != residual {
		t.Fatal("old source budget not consumed once", err)
	}
	var settlement model.SubscriptionValueSettlement
	if err := f.db.Where("subscription_id = ? AND kind = ?", f.subscription, "change").First(&settlement).Error; err != nil || settlement.AmountMinor != residual || settlement.AppliedMinor != 2000 || settlement.WalletMinor != surplus {
		t.Fatalf("untraceable replacement settlement %+v %v", settlement, err)
	}
	values := commerce.SubscriptionValues{Repository: commercestore.SubscriptionValues{DB: f.db, Now: func() time.Time { return *f.at }}}
	nextValue, err := values.Read(t.Context(), 1, sub.ID)
	if err != nil || nextValue.TotalMinor != 2000 {
		t.Fatalf("new purchase lost its actual paid value %+v %v", nextValue, err)
	}
	termination := commerce.SubscriptionTermination{Repository: commercestore.SubscriptionTermination{DB: f.db, Now: func() time.Time { return *f.at }}}
	requestEnd := commerce.SubscriptionTerminationRequest{QuoteFingerprint: nextValue.Fingerprint, IdempotencyKey: "immediate-full-change-unsubscribe"}
	for range 2 {
		if receipt, err := termination.Terminate(t.Context(), 1, sub.ID, requestEnd); err != nil || receipt.AmountMinor != 2000 {
			t.Fatal("new funded service value could not be refunded exactly once", receipt, err)
		}
	}
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&after).Error; err != nil || after.BalanceMinor != 18000+residual || after.ReservedMinor != 0 {
		t.Fatal("replacement/refund money conservation failed", err)
	}

}

func confirmedReplacement(t *testing.T, f fullReplacementFixture, key string) commerce.Order {
	t.Helper()
	request := commerce.OrderCreateRequest{PlanSKUID: f.target, TargetSubscriptionID: f.subscription, IdempotencyKey: key}
	quote, err := f.orders.Preview(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	request.QuoteFingerprint = quote.QuoteFingerprint
	order, err := f.orders.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	return order
}
func TestFullReplacementCancellationAndExpiryPreserveOtherReservations(t *testing.T) {
	for _, expire := range []bool{false, true} {
		t.Run(fmt.Sprint(expire), func(t *testing.T) {
			f := newFullReplacementFixture(t, newOrderFixture(t), false, 0, 3)
			// Another independent purchase reserves existing wallet money.
			other, err := f.orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.base.skuRecord.ID, IdempotencyKey: "other-order"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.payments.Configure(t.Context(), 1, other.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
				t.Fatal(err)
			}
			replacement := confirmedReplacement(t, f, "cancel-or-expire")
			fund, err := f.payments.Configure(t.Context(), 1, replacement.ID, commerce.FundingRequest{UseBalance: true})
			if err != nil {
				t.Fatal(err)
			}
			if fund.EntitlementMinor != 2000 || fund.ReservedMinor != 0 {
				t.Fatal(fund)
			}
			if expire {
				// Expire just this reservation while the unrelated reservation stays live.
				if err := f.db.Model(&model.OrderFunding{}).Where("order_id = ?", other.ID).Update("expires_at", f.at.Add(24*time.Hour)).Error; err != nil {
					t.Fatal(err)
				}
				for range 2 {
					if _, err = f.payments.Expire(t.Context(), f.at.Add(time.Hour), 20); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				cancel := commercestore.OrderCancellation{DB: f.db}
				for range 2 {
					if _, err := cancel.Cancel(t.Context(), 1, replacement.ID, false, false); err != nil {
						t.Fatal(err)
					}
				}
			}
			var wallet model.WalletAccount
			if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 18000 || wallet.ReservedMinor != 12000 {
				t.Fatal("unsettled residual released someone else's reserve", wallet, err)
			}
			var root model.Order
			if err := f.db.First(&root, f.root.ID).Error; err != nil || root.RefundAmount != 0 || root.SubscriptionEndedAt != nil {
				t.Fatal("cancel refunded/ended old service", root, err)
			}
		})
	}
}
func TestFullReplacementPublicationFailureRollsBackMoneyAndEpoch(t *testing.T) {
	f := newFullReplacementFixture(t, newOrderFixture(t), false, 80, 2)
	order := confirmedReplacement(t, f, "publication-rollback")
	if _, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	rejectPublishInsert(t, f.base)
	if _, err := f.payments.Pay(t.Context(), 1, order.ID); err == nil {
		t.Fatal("delivery intent failure was committed")
	}
	var wallet model.WalletAccount
	var root model.Order
	var sub model.Subscription
	var old model.ProtocolCredential
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 18000 || wallet.ReservedMinor != 0 {
		t.Fatal(wallet, err)
	}
	if err := f.db.First(&root, f.root.ID).Error; err != nil || root.RefundAmount != 0 || root.SubscriptionEndedAt != nil {
		t.Fatal(root, err)
	}
	if err := f.db.First(&sub, f.subscription).Error; err != nil || sub.EntitlementEpoch != 0 || sub.PlanID != root.PlanID {
		t.Fatal(sub, err)
	}
	if err := f.db.First(&old, f.oldCredential.ID).Error; err != nil || old.CurrentScope == nil || old.Secret == "" || old.Status != "active" {
		t.Fatal("old access changed on rollback", err)
	}
	for _, table := range []string{"subscription_value_settlements", "closed_entitlements"} {
		var count int64
		if err := f.db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatal(table, count, err)
		}
	}
	if err := f.db.Exec("DROP TRIGGER reject_publish").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Pay(t.Context(), 1, order.ID); err != nil {
		t.Fatal("retry after rollback failed", err)
	}
}
func TestFullReplacementOldFlowOnlyConsumesClosedQuota(t *testing.T) {
	f := newFullReplacementFixture(t, newOrderFixture(t), false, 80, 2)
	order := confirmedReplacement(t, f, "late-old-flow")
	if _, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Pay(t.Context(), 1, order.ID); err != nil {
		t.Fatal(err)
	}
	var fresh model.ProtocolCredential
	if err := f.db.Where("subscription_id = ? AND current_scope = ?", f.subscription, 1).First(&fresh).Error; err != nil {
		t.Fatal(err)
	}
	if fresh.CredentialID == f.oldCredential.CredentialID || fresh.PrincipalKey == f.oldCredential.PrincipalKey || fresh.EntitlementEpoch != 1 {
		t.Fatal("replacement reused old authentication identity")
	}
	accounting := metering.CompletionAccounting{Repository: meteringstore.CompletionAccounting{DB: f.db, Cipher: f.base.h.credentialCipher, EnqueuePublications: networkstore.EnqueueSubscriptionPublications}}
	event := metering.CompletedFlow{NodeID: f.oldCredential.NodeID, CoreInstanceID: "full-change-core", EventID: "late-old-flow", EventType: "flow.completed", Sequence: 2, FlowID: "late-old-flow", PrincipalKey: f.oldCredential.PrincipalKey, BytesDown: 30, OccurredAt: f.at.Add(time.Second)}
	for range 2 {
		if _, _, err := accounting.Complete(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	var sub model.Subscription
	if err := f.db.First(&sub, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	_, used := entitlements.CycleQuota(entitlements.Subscription(sub))
	if used != 0 || sub.Status != "active" {
		t.Fatal("late old flow consumed new service", used, sub.Status)
	}
	var records []model.TrafficRecord
	if err := f.db.Where("subscription_id = ? AND report_id = ?", f.subscription, "late-old-flow").Find(&records).Error; err != nil || len(records) != 1 || records[0].RawBytes != 30 || records[0].UsedBytes != 20 {
		t.Fatal("raw/charged late usage lost or duplicated", records, err)
	}
	var old model.ClosedEntitlement
	if err := f.db.Where("subscription_id = ? AND epoch = ?", f.subscription, 0).First(&old).Error; err != nil || old.FlowUsed != 100 {
		t.Fatal("old flow lost or counted twice", old.FlowUsed, err)
	}
}

func TestFullReplacementMixedPaymentAndCanceledLatePayment(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint(cancel), func(t *testing.T) { assertFullReplacementMixedPayment(t, newOrderFixture(t), cancel) })
	}
}
func TestMySQLFullReplacementMixedPaymentAndCanceledLatePayment(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint(cancel), func(t *testing.T) { assertFullReplacementMixedPayment(t, newMySQLOrderFixture(t), cancel) })
	}
}
func assertFullReplacementMixedPayment(t *testing.T, base orderFixture, cancel bool) {
	f := newFullReplacementFixture(t, base, false, 0, 3)
	if err := f.db.Model(&model.PlanSKU{}).Where("id = ?", f.target).Update("price_cents", 30000).Error; err != nil {
		t.Fatal(err)
	}
	order := confirmedReplacement(t, f, "mixed-full-change")
	fund, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true})
	if err != nil {
		t.Fatal(err)
	}
	if fund.EntitlementMinor != 9000 || fund.ReservedMinor != 18000 || fund.WalletMinor != 27000 || fund.CashMinor != 3000 {
		t.Fatal("wrong mixed contribution", fund)
	}
	if cancel {
		if _, err := (commercestore.OrderCancellation{DB: f.db}).Cancel(t.Context(), 1, order.ID, false, false); err != nil {
			t.Fatal(err)
		}
	}
	facts := commerce.PaymentFacts{Repository: commercestore.PaymentFacts{DB: f.db, Issuer: zeroadapter.Issuer{Cipher: base.h.credentialCipher, Mieru: base.h.zeroMieruAccess}, Now: func() time.Time { return *f.at }}}
	fact := commerce.PaymentFact{OrderID: order.ID, ProviderEventID: "mixed-replacement-cash", ProviderTradeNo: "mixed-replacement-cash", Status: "paid", AmountMinor: 3000, Currency: "CNY", OccurredAt: *f.at}
	for range 2 {
		if _, err := facts.Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: "sandbox-full-change", RequireAdministrator: true}, fact); err != nil {
			t.Fatal(err)
		}
	}
	var wallet model.WalletAccount
	var stored model.Order
	var sub model.Subscription
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&stored, order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&sub, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	if cancel {
		if wallet.BalanceMinor != 21000 || wallet.ReservedMinor != 0 || stored.Status != "canceled" || sub.EntitlementEpoch != 0 {
			t.Fatal("late canceled cash changed rights or was lost/duplicated", wallet, stored.Status, sub.EntitlementEpoch)
		}
	} else {
		if wallet.BalanceMinor != 0 || wallet.ReservedMinor != 0 || stored.Status != "paid" || stored.PaidAmount != 30000 || sub.EntitlementEpoch != 1 {
			t.Fatal("mixed settlement did not conserve money", wallet, stored.Status, stored.PaidAmount, sub.EntitlementEpoch)
		}
	}
}
func TestFullReplacementRejectsUnacceptedAndConsumedQuote(t *testing.T) {
	f := newFullReplacementFixture(t, newOrderFixture(t), true, 80, 2)
	request := commerce.OrderCreateRequest{PlanSKUID: f.target, TargetSubscriptionID: f.subscription}
	if _, err := f.orders.Create(t.Context(), 1, request); err == nil {
		t.Fatal("new full change silently accepted without a quote")
	}
	quote, err := f.orders.Preview(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&model.Subscription{}).Where("id = ?", f.subscription).Update("flow_used", 90).Error; err != nil {
		t.Fatal(err)
	}
	request.QuoteFingerprint = quote.QuoteFingerprint
	if _, err := f.orders.Create(t.Context(), 1, request); err == nil {
		t.Fatal("consumed residual accepted at checkout")
	}
	order := confirmedReplacement(t, f, "usage-after-confirm")
	if _, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&model.Subscription{}).Where("id = ?", f.subscription).Update("flow_used", 95).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Pay(t.Context(), 1, order.ID); err == nil {
		t.Fatal("consumed old value settled at stale price")
	}
	var root model.Order
	if err := f.db.First(&root, f.root.ID).Error; err != nil || root.RefundAmount != 0 {
		t.Fatal("stale quote refunded old source", err)
	}
}

func TestFullReplacementPaymentDelayPreservesAgreedTimeValue(t *testing.T) {
	f := newFullReplacementFixture(t, newOrderFixture(t), false, 0, 3)
	order := confirmedReplacement(t, f, "waiting-is-not-repricing")
	if _, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	// Keep the reservation valid to isolate agreed valuation from hold expiry.
	if err := f.db.Model(&model.OrderFunding{}).Where("order_id = ?", order.ID).Update("expires_at", f.at.Add(48*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	*f.at = f.at.Add(24 * time.Hour)
	if _, err := f.payments.Pay(t.Context(), 1, order.ID); err != nil {
		t.Fatal("waiting alone repriced the agreed quote", err)
	}
	var sub model.Subscription
	if err := f.db.First(&sub, f.subscription).Error; err != nil || !sub.StartAt.Equal(*f.at) || !sub.EndAt.Equal(f.at.AddDate(0, 1, 0)) {
		t.Fatal("new period did not start at fulfillment", err)
	}
	var wallet model.WalletAccount
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 25000 {
		t.Fatal("agreed residual changed with waiting", wallet, err)
	}
}
func TestMySQLFullReplacementConcurrentFinancialTransitions(t *testing.T) {
	for _, scenario := range []string{"duplicate_payment", "two_changes", "unsubscribe", "cancel", "cash_cancel"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFullReplacementFixture(t, newMySQLOrderFixture(t), false, 0, 3)
			if scenario == "cash_cancel" {
				if err := f.db.Model(&model.PlanSKU{}).Where("id = ?", f.target).Update("price_cents", 30000).Error; err != nil {
					t.Fatal(err)
				}
			}
			first := confirmedReplacement(t, f, "race-first-change")
			if _, err := f.payments.Configure(t.Context(), 1, first.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
				t.Fatal(err)
			}
			second := first
			if scenario == "two_changes" {
				second = confirmedReplacement(t, f, "race-second-change")
				if _, err := f.payments.Configure(t.Context(), 1, second.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
					t.Fatal(err)
				}
			}
			values := commerce.SubscriptionValues{Repository: commercestore.SubscriptionValues{DB: f.db, Now: func() time.Time { return *f.at }}}
			quote, err := values.Read(t.Context(), 1, f.subscription)
			if err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			go func() {
				<-start
				if scenario == "cash_cancel" {
					facts := commerce.PaymentFacts{Repository: commercestore.PaymentFacts{DB: f.db, Issuer: zeroadapter.Issuer{Cipher: f.base.h.credentialCipher, Mieru: f.base.h.zeroMieruAccess}, Now: func() time.Time { return *f.at }}}
					_, err := facts.Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: "sandbox-full-change", RequireAdministrator: true}, commerce.PaymentFact{OrderID: first.ID, ProviderEventID: "cash-cancel-race", ProviderTradeNo: "cash-cancel-race", Status: "paid", AmountMinor: 3000, Currency: "CNY", OccurredAt: *f.at})
					results <- err
				} else {
					_, err := f.payments.Pay(t.Context(), 1, first.ID)
					results <- err
				}
			}()
			go func() {
				<-start
				if scenario == "cancel" || scenario == "cash_cancel" {
					_, err := (commercestore.OrderCancellation{DB: f.db}).Cancel(t.Context(), 1, first.ID, false, false)
					results <- err
				} else if scenario == "unsubscribe" {
					termination := commerce.SubscriptionTermination{Repository: commercestore.SubscriptionTermination{DB: f.db, Now: func() time.Time { return *f.at }}}
					_, err := termination.Terminate(t.Context(), 1, f.subscription, commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "race-unsubscribe"})
					results <- err
				} else {
					_, err := f.payments.Pay(t.Context(), 1, second.ID)
					results <- err
				}
			}()
			close(start)
			firstErr, secondErr := <-results, <-results
			if firstErr != nil && secondErr != nil {
				t.Fatal("neither serialized transition completed", firstErr, secondErr)
			}
			var wallet model.WalletAccount
			var sub model.Subscription
			var settlements int64
			if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.db.First(&sub, f.subscription).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.db.Model(&model.SubscriptionValueSettlement{}).Where("subscription_id = ?", f.subscription).Count(&settlements).Error; err != nil {
				t.Fatal(err)
			}
			if scenario == "cancel" || scenario == "cash_cancel" {
				var result model.Order
				if err := f.db.First(&result, first.ID).Error; err != nil {
					t.Fatal(err)
				}
				expectedBalance := int64(25000)
				expectedSettlements := int64(1)
				if scenario == "cash_cancel" {
					expectedBalance = 0
				}
				if result.Status == "canceled" {
					expectedBalance = 18000
					if scenario == "cash_cancel" {
						expectedBalance = 21000
					}
					expectedSettlements = 0
					if sub.EntitlementEpoch != 0 || sub.Status != "active" {
						t.Fatal("cancelled purchase changed old rights", sub.EntitlementEpoch, sub.Status)
					}
				} else if result.Status != "paid" || sub.EntitlementEpoch != 1 {
					t.Fatal("payment/cancel race lost outcome", result.Status, sub.EntitlementEpoch)
				}
				if wallet.BalanceMinor != expectedBalance || wallet.ReservedMinor != 0 || settlements != expectedSettlements {
					t.Fatal("cancel race duplicated/lost financial value", wallet, settlements, firstErr, secondErr)
				}
				return
			}
			expected := int64(25000)
			if sub.Status == "canceled" && sub.EndReason == "canceled" && sub.EndedAt != nil {
				if scenario != "unsubscribe" {
					t.Fatal("unexpected service termination")
				}
				expected = 27000
			} else if sub.EntitlementEpoch != 1 {
				t.Fatal("duplicate/missing replacement epoch", sub.EntitlementEpoch)
			}
			if wallet.BalanceMinor != expected || wallet.ReservedMinor != 0 || settlements != 1 {
				t.Fatal("concurrent transition duplicated financial value", wallet, settlements, firstErr, secondErr)
			}
		})
	}
}

func TestFullReplacementAdministratorDiscountNeverBecomesBalance(t *testing.T) {
	assertReplacementAdministratorDiscount(t, newOrderFixture(t))
}
func TestMySQLFullReplacementAdministratorDiscountNeverBecomesBalance(t *testing.T) {
	assertReplacementAdministratorDiscount(t, newMySQLOrderFixture(t))
}
func assertReplacementAdministratorDiscount(t *testing.T, f orderFixture) {
	attachOrderPublishEndpoint(t, f)
	fundTestWallet(t, f, 1000)
	root := f.create(t, 0)
	payments := fundingService(f)
	if _, err := payments.Configure(t.Context(), 1, root.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := payments.Pay(t.Context(), 1, root.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&root, root.ID).Error; err != nil {
		t.Fatal(err)
	}
	plan := f.plan(t, 2)
	if err := f.h.db.Model(&plan).Update("traffic_bytes", int64(1024)).Error; err != nil {
		t.Fatal(err)
	}
	sku := f.sku(t, plan.ID, 200, skuOperationChange)
	request := commerce.OrderCreateRequest{PlanSKUID: sku.ID, TargetSubscriptionID: root.SubscriptionID}
	quote, err := f.h.services.OrderCreation.Preview(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	payable := int64(50)
	assigned, err := f.h.services.OrderAssignment.Assign(t.Context(), 1, commerce.OrderAssignmentRequest{UserID: 1, PlanSKUID: sku.ID, TargetSubscriptionID: root.SubscriptionID, PayableAmount: &payable, QuoteFingerprint: quote.QuoteFingerprint, Note: "isolated discount conservation test", RequestID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	fund, err := payments.Configure(t.Context(), 1, assigned.ID, commerce.FundingRequest{UseBalance: true})
	if err != nil {
		t.Fatal(err)
	}
	if fund.EntitlementMinor != 50 || fund.SurplusMinor != 50 || fund.CashMinor != 0 {
		t.Fatal("override changed residual accounting", fund)
	}
	if _, err := payments.Pay(t.Context(), 1, assigned.ID); err != nil {
		t.Fatal(err)
	}
	var paid model.Order
	if err := f.h.db.First(&paid, assigned.ID).Error; err != nil || paid.AmountCents != 200 || paid.PaidAmount != 50 || paid.DiscountAmount != 150 {
		t.Fatal("base, discount and actual paid were conflated", paid, err)
	}
	value, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, root.SubscriptionID)
	if err != nil || value.TotalMinor != 50 {
		t.Fatal("commercial discount became refundable cash", value, err)
	}
	if _, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, root.SubscriptionID, commerce.SubscriptionTerminationRequest{QuoteFingerprint: value.Fingerprint, IdempotencyKey: "discount-change-unsubscribe"}); err != nil {
		t.Fatal(err)
	}
	var wallet model.WalletAccount
	if err := f.h.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 1000 || wallet.ReservedMinor != 0 {
		t.Fatal("discount minted wallet value", wallet, err)
	}
}
