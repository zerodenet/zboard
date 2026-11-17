package handler

import (
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestPerpetualFIFOMeteringExhaustionAllowsZeroUnsubscribe(t *testing.T) {
	assertPerpetualFIFOExhaustion(t, newOrderFixture(t))
}
func TestMySQLPerpetualFIFOMeteringExhaustionAllowsZeroUnsubscribe(t *testing.T) {
	assertPerpetualFIFOExhaustion(t, newMySQLBusinessOrderFixture(t))
}
func assertPerpetualFIFOExhaustion(t *testing.T, f orderFixture) {
	t.Helper()
	attachOrderPublishEndpoint(t, f)
	if err := f.h.db.Model(&f.planRecord).Updates(map[string]any{"traffic_bytes": 100, "reset_policy": 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&f.skuRecord).Updates(map[string]any{"price_cents": 1000, "billing_unit": "once", "billing_value": 1, "billing_mode": "one_time", "renewal_effect": "add_quota_only"}).Error; err != nil {
		t.Fatal(err)
	}
	fundTestWallet(t, f, 3000)
	pay := func(order model.Order) model.Order {
		t.Helper()
		payments := fundingService(f)
		if _, err := payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := payments.Pay(t.Context(), 1, order.ID); err != nil {
			t.Fatal(err)
		}
		if err := f.h.db.First(&order, order.ID).Error; err != nil {
			t.Fatal(err)
		}
		return order
	}
	root := pay(f.create(t, 0))
	pay(f.create(t, root.SubscriptionID))
	completePerpetualFIFOUsage(t, f, root.SubscriptionID, "fifo-exhaust-entire-quota", 200)
	var sub model.Subscription
	if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil || sub.EndedAt == nil || sub.EndReason != "exhausted" || sub.Status != "expired" || sub.FlowUsed != 200 || sub.FlowTotal != 200 {
		t.Fatalf("real metering did not exhaust FIFO rights %+v %v", sub, err)
	}
	quote, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, sub.ID)
	if err != nil || quote.TotalMinor != 0 {
		t.Fatalf("exhausted FIFO quote %+v %v", quote, err)
	}
	in := commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "fifo-exhausted-whole-termination"}
	for range 2 {
		receipt, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, sub.ID, in)
		if err != nil || receipt.AmountMinor != 0 || receipt.WalletEntryID != nil || receipt.Status != "canceled" {
			t.Fatalf("exhausted FIFO termination %+v %v", receipt, err)
		}
	}
	var wallet model.WalletAccount
	if err := f.h.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 1000 || wallet.ReservedMinor != 0 {
		t.Fatalf("exhaustion manufactured a refund %+v %v", wallet, err)
	}
	if err := f.h.db.First(&sub, sub.ID).Error; err != nil || sub.Status != "canceled" || sub.EndReason != "canceled" || sub.FlowUsed != 200 {
		t.Fatalf("zero cancellation retained recovery rights %+v %v", sub, err)
	}
}
