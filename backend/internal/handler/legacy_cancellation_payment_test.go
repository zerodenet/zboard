package handler

import (
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestLegacyCancellationRetainsLateMixedCashWithoutRevival(t *testing.T) {
	assertLegacyCancellationLateCash(t, newOrderFixture(t))
}
func TestMySQLLegacyCancellationRetainsLateMixedCashWithoutRevival(t *testing.T) {
	assertLegacyCancellationLateCash(t, newMySQLBusinessOrderFixture(t))
}
func assertLegacyCancellationLateCash(t *testing.T, f orderFixture) {
	t.Helper()
	attachOrderPublishEndpoint(t, f)
	priceForFunding(t, f, 1000)
	fundTestWallet(t, f, 1500)
	root := f.create(t, 0)
	p := fundingService(f)
	if _, err := p.Configure(t.Context(), 1, root.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Pay(t.Context(), 1, root.ID); err != nil {
		t.Fatal(err)
	}
	fact := readValueEvidence(t, f, root.ID)
	pending := f.create(t, fact.SubscriptionID)
	funding, err := p.Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: true})
	if err != nil || funding.CashMinor != 500 || funding.WalletMinor != 500 {
		t.Fatalf("mixed funding %+v %v", funding, err)
	}
	in := entitlements.SubscriptionMutationInput{SubscriptionID: fact.SubscriptionID, Kind: entitlements.SubscriptionCancel, Reason: "whole stop without refund before cash confirmation", IdempotencyKey: "legacy-mixed-cancel", Origin: "plugin:legacy.cancel"}
	r, err := f.h.services.SubscriptionMutations().Apply(t.Context(), 1, in)
	if err != nil || r.Status != "canceled" {
		t.Fatalf("whole stop %+v %v", r, err)
	}
	late := commerce.PaymentFact{OrderID: pending.ID, ProviderEventID: "legacy-mixed-late-event", ProviderTradeNo: "legacy-mixed-late-trade", Status: "paid", AmountMinor: 500, Currency: "CNY", OccurredAt: time.Now().UTC()}
	for range 2 {
		out, err := receiptService(f).Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: "sandbox-legacy-cancel", RequireAdministrator: true}, late)
		if err != nil || out.Disposition != "wallet_credit" || out.Fulfilled {
			t.Fatalf("late cash lost or revived rights %+v %v", out, err)
		}
	}
	var wallet model.WalletAccount
	if err := f.h.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 1000 || wallet.ReservedMinor != 0 {
		t.Fatalf("late mixed cash duplicated or lost %+v %v", wallet, err)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, fact.SubscriptionID).Error; err != nil || sub.Status != "canceled" || sub.EndedAt == nil {
		t.Fatalf("late mixed cash revived subscription %+v %v", sub, err)
	}
	var order model.Order
	if err := f.h.db.First(&order, pending.ID).Error; err != nil || order.Status != "canceled" || order.SubscriptionEndedAt == nil || order.FulfilledAt != nil {
		t.Fatalf("late mixed cash fulfilled old order %+v %v", order, err)
	}
	var refunds int64
	if err := f.h.db.Model(&model.WalletEntry{}).Where("kind = ?", "subscription_refund").Count(&refunds).Error; err != nil || refunds != 0 {
		t.Fatalf("zero command issued refund %d %v", refunds, err)
	}
	if again, err := f.h.services.SubscriptionMutations().Apply(t.Context(), 1, in); err != nil || again.MutationID != r.MutationID {
		t.Fatalf("late cash changed original cancellation receipt %+v %v", again, err)
	}
}
