package handler

import (
	"encoding/json"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
	"time"
)

func TestRenewalIntentCannotConsumeTheSameTargetTwice(t *testing.T) {
	assertRenewalIntent(t, newOrderFixture(t))
}
func TestMySQLRenewalIntentCannotConsumeTheSameTargetTwice(t *testing.T) {
	assertRenewalIntent(t, newMySQLOrderFixture(t))
}
func assertRenewalIntent(t *testing.T, f orderFixture) {
	t.Helper()
	fundTestWallet(t, f, 1000)
	original := f.paid(t, f.create(t, 0).ID)
	var before model.Subscription
	if err := f.h.db.First(&before, original.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	first, second := f.create(t, before.ID), f.create(t, before.ID)
	service := fundingService(f)
	if _, err := service.Configure(t.Context(), 1, first.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Configure(t.Context(), 1, second.ID, commerce.FundingRequest{}); err != nil {
		t.Fatal(err)
	}
	prepareReceiptAttempt(t, f, second.ID, "renewal-intent-channel")
	// Current consumption is not a new business intent or a reason to deny an
	// unchanged renewal. It must also survive the successful extend-only grant.
	if err := f.h.db.Model(&before).Update("flow_used", int64(40)).Error; err != nil {
		t.Fatal(err)
	}
	if out, err := service.Pay(t.Context(), 1, first.ID); err != nil || !out.Fulfilled {
		t.Fatalf("first renewal: %+v %v", out, err)
	}
	fact := commerce.PaymentFact{OrderID: second.ID, ProviderEventID: "stale-renewal-event", ProviderTradeNo: "stale-renewal-trade", Status: "paid", AmountMinor: second.PayableAmount, Currency: second.Currency, OccurredAt: time.Now().UTC()}
	for i := 0; i < 2; i++ {
		out, err := receiptService(f).Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: "renewal-intent-channel", RequireAttempt: true}, fact)
		if err != nil || out.Fulfilled || out.Status != "canceled" || out.Disposition != "wallet_credit" {
			t.Fatalf("stale target repeated renewal or lost actual cash: %+v %v", out, err)
		}
	}
	var after model.Subscription
	if err := f.h.db.First(&after, before.ID).Error; err != nil || !after.EndAt.Equal(before.EndAt.AddDate(0, 1, 0)) || after.FlowUsed != 40 {
		t.Fatalf("double renewal or usage reset: %+v %v", after, err)
	}
	walletMatches(t, f, 1000, 0, 4)
	var count int64
	if err := f.h.db.Model(&model.QuotaEvent{}).Where("subscription_id = ? AND event_type = ?", before.ID, "renewal").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("duplicate renewal quota fact: %d %v", count, err)
	}
}
func TestLegacyPendingRenewalKeepsItsUnversionedAgreement(t *testing.T) {
	f := newOrderFixture(t)
	original := f.paid(t, f.create(t, 0).ID)
	first, legacy := f.create(t, original.SubscriptionID), f.create(t, original.SubscriptionID)
	if err := f.h.db.First(&legacy, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(legacy.PurchaseSnapshot), &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "renewal_target")
	payload, _ := json.Marshal(fields)
	if err := f.h.db.Model(&legacy).Update("purchase_snapshot", string(payload)).Error; err != nil {
		t.Fatal(err)
	}
	f.paid(t, first.ID)
	if out := f.paid(t, legacy.ID); out.Status != "paid" {
		t.Fatal("invented a historical intent and rejected an agreed legacy order")
	}
}
