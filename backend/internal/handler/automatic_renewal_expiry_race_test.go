package handler

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestMySQLExpiryCoordinatesOrderLocksBeforeSubscriptionLocks(t *testing.T) {
	f := expiredRecoveryFixture(t, newMySQLBusinessOrderFixture(t))
	setRecoveryPolicy(t, f, 14)
	type expiryMarker struct{}
	var ordersLocked, reversed atomic.Bool
	const hook = "test:expiry-order-before-subscription"
	if err := f.db.Callback().Query().After("gorm:query").Register(hook, func(q *gorm.DB) {
		lock, ok := q.Statement.Clauses["FOR"].Expression.(clause.Locking)
		if q.Statement.Context.Value(expiryMarker{}) != true || !ok || lock.Strength != "UPDATE" || q.Error != nil || q.RowsAffected == 0 {
			return
		}
		if q.Statement.Table == "orders" {
			ordersLocked.Store(true)
		}
		if q.Statement.Table == "subscriptions" && !ordersLocked.Load() {
			reversed.Store(true)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.db.Callback().Query().Remove(hook)
	if _, err := f.base.h.services.CredentialExpiry(f.base.h.credentialCipher).ExpireDue(context.WithValue(t.Context(), expiryMarker{}, true), *f.at, 20); err != nil {
		t.Fatal(err)
	}
	if reversed.Load() {
		t.Fatal("expiry acquired subscription locks before the related order locks needed by recovery settlement")
	}
}

func TestMySQLAutomaticRenewalVersusCredentialExpiry(t *testing.T) {
	f := expiredRecoveryFixture(t, newMySQLBusinessOrderFixture(t))
	setRecoveryPolicy(t, f, 14)
	service := automaticRenewalsFor(f)
	enableAutomaticRenewal(t, f, service)
	start := make(chan struct{})
	done := make(chan error, 2)
	go func() { <-start; _, err := service.Process(t.Context(), 20); done <- err }()
	go func() {
		<-start
		_, err := f.base.h.services.CredentialExpiry(f.base.h.credentialCipher).ExpireDue(t.Context(), *f.at, 20)
		done <- err
	}()
	close(start)
	failed := false
	for range 2 {
		if err := <-done; err != nil {
			t.Errorf("expiry/automatic-renewal race failed: %v", err)
			failed = true
		}
	}
	if failed {
		return
	}
	var sub model.Subscription
	var wallet model.WalletAccount
	if err := f.db.First(&sub, f.subscription).Error; err != nil || sub.Status != "active" || sub.EntitlementEpoch != 1 || !sub.EndAt.Equal(f.at.AddDate(1, 0, 0)) {
		t.Fatal("expiry changed a completed recovery", sub.Status, sub.EntitlementEpoch, err)
	}
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 6000 || wallet.ReservedMinor != 0 {
		t.Fatal("expiry race changed financial outcome", wallet.BalanceMinor, wallet.ReservedMinor, err)
	}
	var current model.ProtocolCredential
	if err := f.db.Where("subscription_id = ? AND current_scope = ?", sub.ID, 1).First(&current).Error; err != nil || (current.Status != "active" && current.Status != "prepared") || current.Secret == "" {
		t.Fatal("expiry invalidated the new paid credential", current.Status, err)
	}
}

func TestMySQLExpiryRejectsCheckoutCommittedDuringOwnerLockWait(t *testing.T) {
	f := expiredRecoveryFixture(t, newMySQLBusinessOrderFixture(t))
	setRecoveryPolicy(t, f, 14)
	checkout := f.db.Begin()
	if checkout.Error != nil {
		t.Fatal(checkout.Error)
	}
	defer checkout.Rollback()
	var user model.User
	if err := checkout.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, 1).Error; err != nil {
		t.Fatal(err)
	}
	orders := commerce.OrderCreation{Repository: commercestore.OrderCreation{DB: checkout, Now: func() time.Time { return *f.at }}}
	pending, err := orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.base.skuRecord.ID, TargetSubscriptionID: f.subscription})
	if err != nil {
		t.Fatal(err)
	}
	payments := commerce.WalletPayments{Repository: commercestore.WalletPayments{DB: checkout, Now: func() time.Time { return *f.at }}}
	if _, err := payments.Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	type marker struct{}
	locked := make(chan struct{})
	var signaled atomic.Bool
	const hook = "test:expiry-owner-wait-membership"
	if err := f.db.Callback().Query().After("gorm:query").Register(hook, func(q *gorm.DB) {
		lock, ok := q.Statement.Clauses["FOR"].Expression.(clause.Locking)
		if q.Statement.Context.Value(marker{}) == true && q.Statement.Table == "orders" && ok && lock.Strength == "UPDATE" && q.Error == nil && q.RowsAffected > 0 && signaled.CompareAndSwap(false, true) {
			close(locked)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.db.Callback().Query().Remove(hook)
	done := make(chan error, 1)
	go func() {
		_, err := f.base.h.services.CredentialExpiry(f.base.h.credentialCipher).ExpireDue(context.WithValue(t.Context(), marker{}, true), *f.at, 20)
		done <- err
	}()
	select {
	case <-locked:
	case err := <-done:
		t.Fatalf("expiry did not reach the controlled owner wait: %v", err)
	case <-time.After(5 * time.Second):
		_ = checkout.Rollback()
		t.Fatal("expiry did not acquire its prepared order lock")
	}
	if err := checkout.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "membership") {
			t.Fatal("expiry used a snapshot from before the owner wait", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expiry did not reject changed membership")
	}
	var sub model.Subscription
	var wallet model.WalletAccount
	if err := f.db.First(&sub, f.subscription).Error; err != nil || sub.EndedAt != nil {
		t.Fatal("failed scope check committed lifecycle changes", sub.EndedAt, err)
	}
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 18000 || wallet.ReservedMinor != 12000 {
		t.Fatal("failed scope check changed a reservation", wallet.BalanceMinor, wallet.ReservedMinor, err)
	}
	if _, err := f.base.h.services.CredentialExpiry(f.base.h.credentialCipher).ExpireDue(t.Context(), *f.at, 20); err != nil {
		t.Fatal("fresh expiry scope could not recover", err)
	}
	if out, err := f.payments.Pay(t.Context(), 1, pending.ID); err != nil || !out.Fulfilled {
		t.Fatal("valid reserved recovery could not fulfill after reconciliation", out.Status, err)
	}
}
