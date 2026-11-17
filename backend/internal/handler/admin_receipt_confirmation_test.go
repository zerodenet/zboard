package handler

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
	"time"
)

func TestAdministratorReceiptProducesUsableValueAndReplaysOnce(t *testing.T) {
	assertAdministratorReceiptValue(t, newOrderFixture(t))
}
func TestMySQLAdministratorReceiptProducesUsableValueAndReplaysOnce(t *testing.T) {
	assertAdministratorReceiptValue(t, newMySQLBusinessOrderFixture(t))
}
func assertAdministratorReceiptValue(t *testing.T, f orderFixture) {
	priceForFunding(t, f, 20600)
	if err := f.h.db.Model(&f.skuRecord).Updates(map[string]any{"billing_unit": "year", "billing_value": 1}).Error; err != nil {
		t.Fatal(err)
	}
	order := f.create(t, 0)
	// Editing today's catalog must not change the agreed paid order.
	if err := f.h.db.Model(&f.skuRecord).Update("price_cents", 99999).Error; err != nil {
		t.Fatal(err)
	}
	paid := f.paid(t, order.ID)
	value := readValueEvidence(t, f, paid.ID)
	total := int64(0)
	for _, period := range value.PaidPeriods {
		total += period.AmountMinor
	}
	if !value.PaymentVerified || value.PaymentBasis != "verified_cash_or_wallet" || total != 20600 || len(value.PaidPeriods) != 12 || value.ActualPaidMinor != paid.PaidAmount {
		t.Fatalf("manual receipt missing paid facts: %+v", value)
	}
	quote, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, paid.SubscriptionID)
	if err != nil || quote.TotalMinor <= 0 || quote.TotalMinor > 20600 {
		t.Fatalf("confirmed manual receipt cannot be valued: %+v %v", quote, err)
	}
	plan := f.plan(t, 2)
	target := f.sku(t, plan.ID, 22000, "change")
	preview, err := f.h.services.OrderCreation.Preview(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: target.ID, TargetSubscriptionID: paid.SubscriptionID})
	if err != nil || preview.ChangeMode != "full_term" || preview.SettlementAmount <= 0 {
		t.Fatalf("confirmed manual order cannot change plan: %+v %v", preview, err)
	}
	before := paid.EntitlementValueSnapshot
	replay := f.paid(t, order.ID)
	if replay.SubscriptionID != paid.SubscriptionID || replay.EntitlementValueSnapshot != before {
		t.Fatal("confirmation replay rewrote value or granted another subscription")
	}
	for _, row := range []struct {
		table, where string
		args         []any
	}{{"subscriptions", "id=?", []any{paid.SubscriptionID}}, {"quota_events", "subscription_id=? AND reference_type='order' AND reference_id=?", []any{paid.SubscriptionID, fmt.Sprint(order.ID)}}, {"audit_logs", "target=? AND action='order.pay'", []any{fmt.Sprintf("order:%d", order.ID)}}} {
		var n int64
		if e := f.h.db.Table(row.table).Where(row.where, row.args...).Count(&n).Error; e != nil || n != 1 {
			t.Fatalf("replayed %s count=%d err=%v", row.table, n, e)
		}
	}
	// Exercise the real replacement settlement, not only a successful preview.
	changed, err := f.h.services.OrderCreation.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: target.ID, TargetSubscriptionID: paid.SubscriptionID, QuoteFingerprint: preview.QuoteFingerprint})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		f.paid(t, changed.ID)
	}
	replacement := readValueEvidence(t, f, changed.ID)
	if !replacement.PaymentVerified || replacement.Mode != "replacement_term" || len(replacement.PaidPeriods) == 0 {
		t.Fatalf("manual replacement lost future value: %+v", replacement)
	}
	var source model.Order
	if err := f.h.db.First(&source, order.ID).Error; err != nil || source.PaidAmount != 20600 || source.RefundAmount != preview.SettlementAmount {
		t.Fatalf("manual source settled incorrectly: %+v %v", source, err)
	}
	var settlements int64
	if err := f.h.db.Model(&model.SubscriptionValueSettlement{}).Where("subscription_id=?", paid.SubscriptionID).Count(&settlements).Error; err != nil || settlements != 1 {
		t.Fatalf("replacement settled %d times: %v", settlements, err)
	}

}
func TestAdministratorReceiptCapturesReservedWalletAndTopupOnce(t *testing.T) {
	f := newOrderFixture(t)
	priceForFunding(t, f, 5000)
	fundTestWallet(t, f, 3000)
	order := f.create(t, 0)
	if _, err := fundingService(f).Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if out := f.pay(t, order.ID, false); out.Code != 200 {
			t.Fatal(out.Body.String())
		}
	}
	walletMatches(t, f, 0, 0, 3)
	var funding model.OrderFunding
	if err := f.h.db.First(&funding, "order_id=?", order.ID).Error; err != nil || funding.State != "captured" {
		t.Fatalf("funding not settled: %+v %v", funding, err)
	}
	value := readValueEvidence(t, f, order.ID)
	if !value.PaymentVerified || value.ActualPaidMinor != 5000 {
		t.Fatal("mixed manual receipt did not preserve full payment")
	}
	topup := createTopUp(t, f, 2100, "admin-receipt-topup")
	provider := "manual-topup-later-callback"
	prepareReceiptAttempt(t, f, topup.ID, provider)
	for range 2 {
		if out := f.pay(t, topup.ID, false); out.Code != 200 {
			t.Fatal(out.Body.String())
		}
	}
	walletMatches(t, f, 2100, 0, 4)
	// A later provider callback must not count the same manually confirmed cash twice.
	fact := commerce.PaymentFact{OrderID: topup.ID, ProviderEventID: "later-manual-topup-event", ProviderTradeNo: "later-manual-topup-trade", Status: "paid", AmountMinor: 2100, Currency: "CNY", OccurredAt: time.Now().UTC()}
	for range 2 {
		result, err := receiptService(f).Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: provider, RequireAttempt: true}, fact)
		if err != nil || result.Disposition != "review_required" || result.Fulfilled {
			t.Fatalf("later receipt duplicated manual credit: %+v %v", result, err)
		}
	}
	walletMatches(t, f, 2100, 0, 4)
}
func TestAdministratorReceiptEvidenceFailureRollsBackEverything(t *testing.T) {
	f := newOrderFixture(t)
	order := f.create(t, 0)
	if err := f.h.db.Exec(`CREATE TRIGGER reject_admin_value BEFORE UPDATE OF entitlement_value_snapshot ON orders BEGIN SELECT RAISE(ABORT, 'financial evidence unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if out := f.pay(t, order.ID, false); out.Code == 200 {
		t.Fatal("accepted without value evidence")
	}
	var actual model.Order
	if err := f.h.db.First(&actual, order.ID).Error; err != nil || actual.Status != "pending" || actual.SubscriptionID != 0 || actual.EntitlementValueSnapshot != "" {
		t.Fatalf("partial order: %+v %v", actual, err)
	}
	for _, table := range []string{"subscriptions", "quota_events", "audit_logs"} {
		var n int64
		if err := f.h.db.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("partial %s: %d %v", table, n, err)
		}
	}
}
