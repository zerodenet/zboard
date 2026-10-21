package handler

import (
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func setRecoveryPolicy(t *testing.T, f fullReplacementFixture, days int) {
	t.Helper()
	raw, err := json.Marshal(days)
	if err != nil {
		t.Fatal(err)
	}
	var row model.SystemConfig
	if err := f.db.Where("config_key = ?", entitlements.RecoveryGraceDaysKey).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.base.h.services.SettingUpdate(f.base.h.credentialCipher).Update(t.Context(), 1, platform.SettingUpdateInput{Key: row.ConfigKey, Value: raw, ExpectedRevision: &row.Revision}); err != nil {
		t.Fatal(err)
	}
}
func expiredRecoveryFixture(t *testing.T, base orderFixture) fullReplacementFixture {
	t.Helper()
	f := newFullReplacementFixtureInYear(t, base, false, 0, 0, 2025)
	// Advance past the real annual purchase's end without rewriting paid facts.
	*f.at = time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC)
	f.base.h.services.SubscriptionQueries = entitlements.SubscriptionQueries{Repository: entitlementstore.SubscriptionQueries{DB: f.db, Now: func() time.Time { return *f.at }}}
	return f
}
func TestConfiguredRecoveryQuoteListFulfillmentAndCleanup(t *testing.T) {
	base := newOrderFixture(t)
	prepareTestMaintenanceSource(t, base.h)
	assertConfiguredRecovery(t, base)
}
func TestMySQLConfiguredRecoveryQuoteListFulfillmentAndCleanup(t *testing.T) {
	assertConfiguredRecovery(t, newMySQLOrderFixture(t))
}
func assertConfiguredRecovery(t *testing.T, base orderFixture) {
	f := expiredRecoveryFixture(t, base)
	request := commerce.OrderCreateRequest{PlanSKUID: f.base.skuRecord.ID, TargetSubscriptionID: f.subscription}
	if _, err := f.orders.Preview(t.Context(), 1, request); err == nil {
		t.Fatal("default seven-day window admitted day eight")
	}
	setRecoveryPolicy(t, f, 14)
	page, err := f.base.h.services.SubscriptionQueries.Owned(t.Context(), 1, entitlements.SubscriptionQuery{EligibleFor: "renew", Limit: 20})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || !page.Items[0].CanRenew || page.Items[0].RenewalUntil == nil || !page.Items[0].RenewalUntil.Equal(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("candidate/deadline disagrees with active policy", page, err)
	}
	expiry := f.base.h.services.CredentialExpiry(f.base.h.credentialCipher)
	if _, err := expiry.ExpireDue(t.Context(), *f.at, 20); err != nil {
		t.Fatal(err)
	}
	var retained model.ProtocolCredential
	if err := f.db.First(&retained, f.oldCredential.ID).Error; err != nil || retained.Secret == "" || retained.CurrentScope == nil {
		t.Fatal("custom window prematurely destroyed recovery identity", err)
	}
	quote, err := f.orders.Preview(t.Context(), 1, request)
	if err != nil || quote.AmountCents != 12000 || quote.PayableAmount != 12000 {
		t.Fatal("recovery lost purchased base price", quote, err)
	}
	request.QuoteFingerprint = quote.QuoteFingerprint
	order, err := f.orders.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := f.payments.Pay(t.Context(), 1, order.ID); err != nil {
			t.Fatal(err)
		}
	}
	var sub model.Subscription
	if err := f.db.First(&sub, f.subscription).Error; err != nil || sub.EntitlementEpoch != 1 || sub.Status != "active" || !sub.StartAt.Equal(*f.at) || !sub.EndAt.Equal(f.at.AddDate(1, 0, 0)) {
		t.Fatal("configured recovery did not produce exactly one full period", sub, err)
	}
	var wallet model.WalletAccount
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 6000 || wallet.ReservedMinor != 0 {
		t.Fatal("recovery duplicated payment", wallet, err)
	}
}
func TestRecoveryWindowChangeKeepsLateCashWithoutRestoringService(t *testing.T) {
	base := newOrderFixture(t)
	prepareTestMaintenanceSource(t, base.h)
	assertRecoveryLateCash(t, base)
}
func TestMySQLRecoveryWindowChangeKeepsLateCashWithoutRestoringService(t *testing.T) {
	assertRecoveryLateCash(t, newMySQLOrderFixture(t))
}
func assertRecoveryLateCash(t *testing.T, base orderFixture) {
	f := expiredRecoveryFixture(t, base)
	setRecoveryPolicy(t, f, 14)
	request := commerce.OrderCreateRequest{PlanSKUID: f.base.skuRecord.ID, TargetSubscriptionID: f.subscription, IdempotencyKey: "recover-late-cash-order"}
	order, err := f.orders.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: false}); err != nil {
		t.Fatal(err)
	}
	setRecoveryPolicy(t, f, 7)
	facts := commerce.PaymentFacts{Repository: commercestore.PaymentFacts{DB: f.db, Issuer: zeroadapter.Issuer{Cipher: base.h.credentialCipher, Mieru: base.h.zeroMieruAccess}, Now: func() time.Time { return *f.at }}}
	fact := commerce.PaymentFact{OrderID: order.ID, ProviderEventID: "recovery-policy-late-cash", ProviderTradeNo: "recovery-policy-late-cash", Status: "paid", AmountMinor: 12000, Currency: "CNY", OccurredAt: *f.at}
	for range 2 {
		receipt, err := facts.Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: "sandbox-recovery-window", RequireAdministrator: true}, fact)
		if err != nil || receipt.Disposition != "wallet_credit" || receipt.Fulfilled {
			t.Fatal("late cash was lost or restored expired rights", receipt, err)
		}
	}
	var sub model.Subscription
	if err := f.db.First(&sub, f.subscription).Error; err != nil || sub.EntitlementEpoch != 0 || sub.StartAt.Equal(*f.at) {
		t.Fatal("late payment restored closed window", sub, err)
	}
	var wallet model.WalletAccount
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 30000 || wallet.ReservedMinor != 0 {
		t.Fatal("late cash did not enter wallet exactly once", wallet, err)
	}
}
func TestRecoveryPolicyCorruptionNeverDefaultsAndNeverResurrectsTerminal(t *testing.T) {
	f := expiredRecoveryFixture(t, newOrderFixture(t))
	prepareTestMaintenanceSource(t, f.base.h)
	setRecoveryPolicy(t, f, 0)
	if _, err := f.base.h.services.CredentialExpiry(f.base.h.credentialCipher).ExpireDue(t.Context(), *f.at, 20); err != nil {
		t.Fatal(err)
	}
	var retired model.Subscription
	if err := f.db.First(&retired, f.subscription).Error; err != nil || retired.Status != "ended" {
		t.Fatal("disabled window did not end old service", retired.Status, err)
	}
	setRecoveryPolicy(t, f, 14)
	if _, err := f.orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.base.skuRecord.ID, TargetSubscriptionID: f.subscription}); err == nil {
		t.Fatal("extension resurrected terminal service")
	}
	// Missing initialized source is a fault, not permission to use seven days.
	if err := f.db.Where("config_key = ?", entitlements.RecoveryGraceDaysKey).Delete(&model.SystemConfig{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := entitlementstore.RecoveryGracePeriod(f.db); err == nil {
		t.Fatal("runtime source loss silently defaulted")
	}
	if err := datastore.RunMigrations(f.db); err == nil {
		t.Fatal("startup recreated a lost initialized window")
	}
}
func TestRecoveryPolicyConfigurationRequiresAdministrator(t *testing.T) {
	f := expiredRecoveryFixture(t, newOrderFixture(t))
	other := model.User{Email: "recovery-reader@example.test", Password: "unused", Status: "active"}
	if err := f.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.base.h.services.SettingUpdate(f.base.h.credentialCipher).Update(t.Context(), other.ID, platform.SettingUpdateInput{Key: entitlements.RecoveryGraceDaysKey, Value: json.RawMessage(`14`)}); !errors.Is(err, platform.ErrSettingsPermission) {
		t.Fatal("ordinary account changed recovery policy", err)
	}
	if _, err := f.base.h.services.SettingUpdate(f.base.h.credentialCipher).Update(t.Context(), 1, platform.SettingUpdateInput{Key: entitlements.RecoveryGraceDaysKey, Value: json.RawMessage(`-1`)}); err == nil {
		t.Fatal("negative cleanup window accepted")
	}
	grace, err := entitlementstore.RecoveryGracePeriod(f.db)
	if err != nil || grace != 7*24*time.Hour {
		t.Fatal("rejected configuration changed active policy", grace, err)
	}
}

func TestMySQLRecoveryDeadlineRefreshesAfterPolicyLockWait(t *testing.T) {
	f := expiredRecoveryFixture(t, newMySQLOrderFixture(t))
	setRecoveryPolicy(t, f, 14)
	order, err := f.orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.base.skuRecord.ID, TargetSubscriptionID: f.subscription})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&model.OrderFunding{}).Where("order_id = ?", order.ID).Update("expires_at", f.at.Add(60*24*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	blocker := f.db.Begin()
	if blocker.Error != nil {
		t.Fatal(blocker.Error)
	}
	defer blocker.Rollback()
	var policy model.SystemConfig
	if err := blocker.Clauses(clause.Locking{Strength: "UPDATE"}).Where("config_key = ?", entitlements.RecoveryGraceDaysKey).First(&policy).Error; err != nil {
		t.Fatal(err)
	}
	var decision atomic.Int64
	deadline := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	decision.Store(deadline.Add(-time.Second).UnixMilli())
	payments := commerce.WalletPayments{Repository: commercestore.WalletPayments{DB: f.db, Issuer: zeroadapter.Issuer{Cipher: f.base.h.credentialCipher, Mieru: f.base.h.zeroMieruAccess}, Now: func() time.Time { return time.UnixMilli(decision.Load()).UTC() }}}
	type marker struct{}
	ready := make(chan struct{})
	var signaled atomic.Bool
	const hook = "test:recovery-policy-read-wait"
	if err := f.db.Callback().Query().Before("gorm:query").Register(hook, func(q *gorm.DB) {
		lock, ok := q.Statement.Clauses["FOR"].Expression.(clause.Locking)
		if q.Statement.Context.Value(marker{}) == true && q.Statement.Table == "system_configs" && ok && lock.Strength == "SHARE" && signaled.CompareAndSwap(false, true) {
			close(ready)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.db.Callback().Query().Remove(hook)
	done := make(chan error, 1)
	go func() {
		_, err := payments.Pay(context.WithValue(t.Context(), marker{}, true), 1, order.ID)
		done <- err
	}()
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("policy lock wait was not exercised")
	}
	decision.Store(deadline.Add(time.Second).UnixMilli())
	if err := blocker.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		var invalid *commerce.ValidationError
		if !errors.As(err, &invalid) || !strings.Contains(invalid.Message, "续费保留期") {
			t.Fatal("post-lock decision did not reject the recovery deadline", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("payment did not leave the known policy lock")
	}
	var stored model.Order
	var wallet model.WalletAccount
	var sub model.Subscription
	if err := f.db.First(&stored, order.ID).Error; err != nil || stored.Status != "pending" || stored.PaidAmount != 0 {
		t.Fatal("expired decision changed order", stored, err)
	}
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 18000 || wallet.ReservedMinor != 12000 {
		t.Fatal("expired decision captured money", wallet, err)
	}
	if err := f.db.First(&sub, f.subscription).Error; err != nil || sub.EntitlementEpoch != 0 {
		t.Fatal("expired decision granted service", sub.EntitlementEpoch, err)
	}
	if _, err := (commercestore.OrderCancellation{DB: f.db}).Cancel(t.Context(), 1, order.ID, false, false); err != nil {
		t.Fatal(err)
	}
}
func TestLegacyUnboundRenewalCannotPickAnotherSubscription(t *testing.T) {
	base := newOrderFixture(t)
	prepareTestMaintenanceSource(t, base.h)
	assertUnboundLegacyRenewal(t, base)
}
func TestMySQLLegacyUnboundRenewalCannotPickAnotherSubscription(t *testing.T) {
	assertUnboundLegacyRenewal(t, newMySQLOrderFixture(t))
}
func assertUnboundLegacyRenewal(t *testing.T, base orderFixture) {
	f := expiredRecoveryFixture(t, base)
	setRecoveryPolicy(t, f, 14)
	order, err := f.orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.base.skuRecord.ID, TargetSubscriptionID: f.subscription})
	if err != nil {
		t.Fatal(err)
	}
	// Explicitly emulate an old pending row that never captured its target.
	if err := f.db.Model(&model.Order{}).Where("id = ?", order.ID).Updates(map[string]any{"target_subscription_id": nil, "purchase_snapshot": ""}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: false}); err != nil {
		t.Fatal(err)
	}
	facts := commerce.PaymentFacts{Repository: commercestore.PaymentFacts{DB: f.db, Issuer: zeroadapter.Issuer{Cipher: base.h.credentialCipher, Mieru: base.h.zeroMieruAccess}, Now: func() time.Time { return *f.at }}}
	fact := commerce.PaymentFact{OrderID: order.ID, ProviderEventID: "legacy-unbound-recovery", ProviderTradeNo: "legacy-unbound-recovery", Status: "paid", AmountMinor: 12000, Currency: "CNY", OccurredAt: *f.at}
	for range 2 {
		out, err := facts.Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: "sandbox-recovery-window", RequireAdministrator: true}, fact)
		if err != nil || out.Fulfilled || out.Disposition != "wallet_credit" {
			t.Fatal("legacy missing target guessed service identity", out, err)
		}
	}
	var reviewed model.Order
	if err := f.db.First(&reviewed, order.ID).Error; err != nil || !strings.Contains(reviewed.FailureReason, "缺少独立订阅目标") {
		t.Fatal("legacy identity problem was not explained", reviewed.FailureReason, err)
	}
	var count int64
	if err := f.db.Model(&model.Subscription{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("old payment manufactured a new subscription", count, err)
	}
	var wallet model.WalletAccount
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 30000 || wallet.ReservedMinor != 0 {
		t.Fatal("legacy cash was lost/duplicated", wallet, err)
	}
}

func TestMySQLVerifiedCashWaitPastFundingExpiryBecomesWalletCredit(t *testing.T) {
	f := expiredRecoveryFixture(t, newMySQLOrderFixture(t))
	setRecoveryPolicy(t, f, 14)
	order, err := f.orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.base.skuRecord.ID, TargetSubscriptionID: f.subscription})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: false}); err != nil {
		t.Fatal(err)
	}
	expires := f.at.Add(time.Second)
	if err := f.db.Model(&model.OrderFunding{}).Where("order_id = ?", order.ID).Update("expires_at", expires).Error; err != nil {
		t.Fatal(err)
	}
	blocker := f.db.Begin()
	if blocker.Error != nil {
		t.Fatal(blocker.Error)
	}
	defer blocker.Rollback()
	var policy model.SystemConfig
	if err := blocker.Clauses(clause.Locking{Strength: "UPDATE"}).Where("config_key = ?", entitlements.RecoveryGraceDaysKey).First(&policy).Error; err != nil {
		t.Fatal(err)
	}
	var current atomic.Int64
	current.Store(f.at.UnixMilli())
	clock := func() time.Time { return time.UnixMilli(current.Load()).UTC() }
	facts := commerce.PaymentFacts{Repository: commercestore.PaymentFacts{DB: f.db, Issuer: zeroadapter.Issuer{Cipher: f.base.h.credentialCipher, Mieru: f.base.h.zeroMieruAccess}, Now: clock}}
	type marker struct{}
	ready := make(chan struct{})
	var signaled atomic.Bool
	const hook = "test:recovery-funding-wait"
	if err := f.db.Callback().Query().Before("gorm:query").Register(hook, func(q *gorm.DB) {
		lock, ok := q.Statement.Clauses["FOR"].Expression.(clause.Locking)
		if q.Statement.Context.Value(marker{}) == true && q.Statement.Table == "system_configs" && ok && lock.Strength == "SHARE" && signaled.CompareAndSwap(false, true) {
			close(ready)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.db.Callback().Query().Remove(hook)
	type result struct {
		out commerce.PaymentFactResult
		err error
	}
	done := make(chan result, 1)
	fact := commerce.PaymentFact{OrderID: order.ID, ProviderEventID: "policy-lock-expired-funding", ProviderTradeNo: "policy-lock-expired-funding", Status: "paid", AmountMinor: 12000, Currency: "CNY", OccurredAt: *f.at}
	authority := commerce.PaymentFactAuthority{AccountID: 1, Provider: "sandbox-recovery-window", RequireAdministrator: true}
	go func() {
		out, err := facts.Record(context.WithValue(t.Context(), marker{}, true), authority, fact)
		done <- result{out, err}
	}()
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("cash policy lock wait was not exercised")
	}
	current.Store(expires.Add(time.Second).UnixMilli())
	if err := blocker.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil || got.out.Fulfilled || got.out.Disposition != "wallet_credit" {
			t.Fatal("known stale funding lost verified cash", got.out, got.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cash did not leave policy lock")
	}
	if out, err := facts.Record(t.Context(), authority, fact); err != nil || out.Disposition != "wallet_credit" || out.Fulfilled {
		t.Fatal("late cash replay changed disposition", out, err)
	}
	var wallet model.WalletAccount
	var sub model.Subscription
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 30000 || wallet.ReservedMinor != 0 {
		t.Fatal("verified cash lost or credited twice", wallet, err)
	}
	if err := f.db.First(&sub, f.subscription).Error; err != nil || sub.EntitlementEpoch != 0 {
		t.Fatal("expired funding granted a new period", sub.EntitlementEpoch, err)
	}
}
