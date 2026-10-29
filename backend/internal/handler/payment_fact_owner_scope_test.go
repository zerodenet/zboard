package handler

import (
	"errors"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestPaymentFactScopePreservesOwnerOnlyAuthorityAndCurrentAdmin(t *testing.T) {
	assertPaymentFactOwnerOnlyScope(t, settlementScopeSQLiteFixture(t))
}

func TestMySQLPaymentFactScopePreservesOwnerOnlyAuthorityAndCurrentAdmin(t *testing.T) {
	assertPaymentFactOwnerOnlyScope(t, newMySQLBusinessOrderFixture(t))
}

func assertPaymentFactOwnerOnlyScope(t *testing.T, base orderFixture) {
	f := expiredRecoveryFixture(t, base)
	setRecoveryPolicy(t, f, 14)
	pending, err := f.orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.base.skuRecord.ID, TargetSubscriptionID: f.subscription})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: false}); err != nil {
		t.Fatal(err)
	}
	var before model.Subscription
	if err := f.db.First(&before, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	other := model.User{Email: "different-payment-admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
	if err := f.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	facts := receiptServiceForScope(f)
	fact := commerce.PaymentFact{OrderID: pending.ID, ProviderEventID: "owner-only-event", ProviderTradeNo: "owner-only-trade", Status: "paid", AmountMinor: 12000, Currency: "CNY", OccurredAt: *f.at}
	for _, admin := range []bool{true, false} {
		authority := commerce.PaymentFactAuthority{AccountID: other.ID, Provider: "owner-only-authority", RequireAdministrator: admin}
		if _, err := facts.Record(t.Context(), authority, fact); !errors.Is(err, commerce.ErrOrderPermission) {
			t.Fatal("foreign account recorded an order using an admin or ordinary authority", admin, err)
		}
	}
	if err := f.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := facts.Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: "owner-only-authority", RequireAdministrator: true}, fact); !errors.Is(err, commerce.ErrOrderPermission) {
		t.Fatal("source preparation bypassed current administrator revocation", err)
	}
	for _, table := range []any{&model.PaymentEvent{}, &model.PaymentReceipt{}} {
		var count int64
		if err := f.db.Model(table).Where("order_id = ?", pending.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("denied source authority persisted a payment identity", count, err)
		}
	}
	var saved model.Order
	if err := f.db.First(&saved, pending.ID).Error; err != nil || saved.Status != "pending" || saved.PaidAmount != 0 || saved.SubscriptionID != 0 {
		t.Fatal("denied authority changed the order", saved, err)
	}
	var wallet model.WalletAccount
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 18000 || wallet.ReservedMinor != 0 {
		t.Fatal("denied authority changed buyer funds", wallet, err)
	}
	var sub model.Subscription
	if err := f.db.First(&sub, f.subscription).Error; err != nil || sub.EntitlementEpoch != before.EntitlementEpoch || sub.Status != before.Status || !sub.StartAt.Equal(before.StartAt) || !sub.EndAt.Equal(before.EndAt) || sub.FlowTotal != before.FlowTotal || sub.FlowUsed != before.FlowUsed {
		t.Fatal("denied authority changed old rights", sub, err)
	}
}
