package handler

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func accessExpiryStore(f orderFixture) entitlementstore.CredentialExpiry {
	return entitlementstore.CredentialExpiry{DB: f.h.db, Cipher: f.h.credentialCipher, Publish: networkstore.EnqueueNodePublication,
		BeforeRetire: commercestore.CancelRetiredSubscriptionOrders,
		PrepareScope: func(ctx context.Context, subs []model.Subscription) (entitlementstore.CredentialExpiryScope, error) {
			return commercestore.PrepareLifecycleClosureScope(f.h.db.WithContext(ctx), subs)
		}}
}
func accessExpiryPaidRoot(t *testing.T, f orderFixture, key string) model.Subscription {
	t.Helper()
	order, err := f.h.services.OrderCreation.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, IdempotencyKey: key})
	if err != nil {
		t.Fatal(err)
	}
	wallet := fundingService(f)
	if _, err := wallet.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := wallet.Pay(t.Context(), 1, order.ID); err != nil {
		t.Fatal(err)
	}
	var paid model.Order
	var sub model.Subscription
	if err := f.h.db.First(&paid, order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	return sub
}
func TestCredentialAccessExpiryPassesFinancialBudgetAndCredentialPage(t *testing.T) {
	assertAccessExpiryBudgetPage(t, newOrderFixture(t))
}
func TestMySQLCredentialAccessExpiryPassesFinancialBudgetAndCredentialPage(t *testing.T) {
	assertAccessExpiryBudgetPage(t, newMySQLBusinessOrderFixture(t))
}
func assertAccessExpiryBudgetPage(t *testing.T, f orderFixture) {
	t.Helper()
	attachOrderPublishEndpoint(t, f)
	if err := f.h.db.Model(&f.planRecord).Update("is_renewable", false).Error; err != nil {
		t.Fatal(err)
	}
	fundTestWallet(t, f, 30000)
	bad := accessExpiryPaidRoot(t, f, "access-expiry-low-paid")
	var credential model.ProtocolCredential
	if err := f.h.db.Where("subscription_id=?", bad.ID).First(&credential).Error; err != nil {
		t.Fatal(err)
	}
	//199 old subscription/credential metadata rows are explicitly not paid facts.
	// Together with the real lower root they fill the first200 routing identities.
	legacy := make([]model.Subscription, 199)
	for n := range legacy {
		legacy[n] = model.Subscription{UserID: bad.UserID, PlanID: bad.PlanID, PlanSKUID: bad.PlanSKUID, NodeGroupID: bad.NodeGroupID,
			Lifecycle: "fixed", Status: "active", StartAt: bad.StartAt, EndAt: bad.EndAt, FlowTotal: bad.FlowTotal, Config: "{}"}
	}
	// This is unpaid routing metadata, not the financial/expiry operation under
	// test. Batch only its setup in one transaction, retaining all199 identities.
	if err := f.h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.CreateInBatches(&legacy, 100).Error; err != nil {
			return err
		}
		credentials := make([]model.ProtocolCredential, len(legacy))
		for n, sub := range legacy {
			plain := fmt.Sprintf("legacy-expiry-routing-%d", sub.ID)
			secret, err := f.h.credentialCipher.Encrypt(plain)
			if err != nil {
				return err
			}
			credentials[n] = model.ProtocolCredential{SubscriptionID: sub.ID, UserID: sub.UserID, NodeID: credential.NodeID, ProtocolEndpointID: credential.ProtocolEndpointID,
				CredentialID: plain, PrincipalKey: plain, Secret: secret, Status: "active", ExpiresAt: sub.EndAt}
		}
		return tx.CreateInBatches(&credentials, 100).Error
	}); err != nil {
		t.Fatal("create all199 unpaid routing subscription/credential identities", err)
	}
	healthy := accessExpiryPaidRoot(t, f, "access-expiry-higher-paid")
	rows := make([]model.Order, 1001)
	for n := range rows {
		target := bad.ID
		rows[n] = model.Order{UserID: bad.UserID, TargetSubscriptionID: &target, PlanID: bad.PlanID, PlanSKUID: bad.PlanSKUID, TradeNo: fmt.Sprintf("access-expiry-pending-%04d", n), OrderType: "renewal", Status: "pending", Currency: "CNY"}
	}
	if err := f.h.db.CreateInBatches(&rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	now := healthy.EndAt.Add(time.Hour)
	store := accessExpiryStore(f)
	money := lifecyclePageMoney(t, f.h.db)
	var ordersBefore []model.Order
	var fundsBefore []model.OrderFunding
	var publicationBefore model.NodeConfigPublish
	if err := f.h.db.Order("id").Find(&ordersBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Order("order_id").Find(&fundsBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&publicationBefore, "node_id=?", credential.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExpireDueCredentials(t.Context(), now, 1); !errors.Is(err, commerce.ErrFundingConflict) {
		t.Fatal("fixture did not reach the real1001 mutable budget", err)
	}
	var beforeSubs []model.Subscription
	if err := f.h.db.Order("id").Find(&beforeSubs).Error; err != nil {
		t.Fatal(err)
	}
	queries := 0
	const hook = "test:access-expiry-no-financial-read"
	if err := f.h.db.Callback().Query().Before("gorm:query").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table == "orders" || tx.Statement.Table == "order_fundings" || tx.Statement.Table == "wallet_accounts" {
			queries++
			tx.AddError(errors.New("access expiry read a financial history"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.h.db.Callback().Query().Remove(hook) })
	service := entitlements.CredentialExpiry{Repository: store}
	if n, err := service.ExpireAccessDue(t.Context(), now, 200); err != nil || n != 200 {
		t.Fatal("first bounded credential page", n, err)
	}
	var last model.ProtocolCredential
	if err := f.h.db.Where("subscription_id=?", healthy.ID).First(&last).Error; err != nil || last.Status != "active" {
		t.Fatal("fixture healthy row was not after first200", last.Status, err)
	}
	// New repository object proves progress is durable statuses, not a local cursor.
	restarted := entitlements.CredentialExpiry{Repository: accessExpiryStore(f)}
	if n, err := restarted.ExpireAccessDue(t.Context(), now, 200); err != nil || n != 1 {
		t.Fatal("restarted second credential page", n, err)
	}
	if n, err := restarted.ExpireAccessDue(t.Context(), now, 200); err != nil || n != 0 {
		t.Fatal("expiry intent replay changed result", n, err)
	}
	if queries != 0 {
		t.Fatal("unexpected financial reads", queries)
	}
	if err := f.h.db.Callback().Query().Remove(hook); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(money, lifecyclePageMoney(t, f.h.db)) {
		t.Fatal("access stop changed money or paid evidence")
	}
	var ordersAfter []model.Order
	var fundsAfter []model.OrderFunding
	if err := f.h.db.Order("id").Find(&ordersAfter).Error; err != nil || !reflect.DeepEqual(ordersBefore, ordersAfter) {
		t.Fatal("access pass changed pending/paid order state", err)
	}
	if err := f.h.db.Order("order_id").Find(&fundsAfter).Error; err != nil || !reflect.DeepEqual(fundsBefore, fundsAfter) {
		t.Fatal("access pass changed funding state", err)
	}
	var afterSubs []model.Subscription
	if err := f.h.db.Order("id").Find(&afterSubs).Error; err != nil || !reflect.DeepEqual(beforeSubs, afterSubs) {
		t.Fatal("access pass claimed financial termination", err)
	}
	if err := f.h.db.Where("subscription_id=?", healthy.ID).First(&last).Error; err != nil || last.Status != "expired" || last.Secret == "" {
		t.Fatal("healthy access not locally expired or identity prematurely erased", last.Status, err)
	}
	var publication model.NodeConfigPublish
	if err := f.h.db.First(&publication, "node_id=?", credential.NodeID).Error; err != nil || publication.Generation <= publicationBefore.Generation {
		t.Fatal("durable publication intent missing", publication, err)
	}
	var audit int64
	if err := f.h.db.Model(&model.AuditLog{}).Where("action=? AND target=?", "subscription.access_expire_intent", fmt.Sprintf("subscription:%d", healthy.ID)).Count(&audit).Error; err != nil || audit != 1 {
		t.Fatal("missing local-intent audit", audit, err)
	}
	// The financial guard itself remains strict even while a future retry keeps
	// this oversized subscription out of the normal candidate page.
	if err := f.h.db.Transaction(func(tx *gorm.DB) error {
		return commercestore.CancelRetiredSubscriptionOrders(tx, bad, now)
	}); !errors.Is(err, commerce.ErrFundingConflict) {
		t.Fatal("financial guard was relaxed by access pass", err)
	}
}

func TestCredentialAccessExpiryKeepsPublicationRollbackAndRechecksRenewal(t *testing.T) {
	assertAccessExpiryRollbackRenewal(t, newOrderFixture(t))
}
func TestMySQLCredentialAccessExpiryKeepsPublicationRollbackAndRechecksRenewal(t *testing.T) {
	assertAccessExpiryRollbackRenewal(t, newMySQLBusinessOrderFixture(t))
}
func assertAccessExpiryRollbackRenewal(t *testing.T, f orderFixture) {
	t.Helper()
	attachOrderPublishEndpoint(t, f)
	fundTestWallet(t, f, 30000)
	sub := accessExpiryPaidRoot(t, f, "access-expiry-renew-root")
	now := sub.EndAt.Add(time.Hour)
	store := accessExpiryStore(f)
	var before []model.ProtocolCredential
	var publications []model.NodeConfigPublish
	if err := f.h.db.Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Order("node_id").Find(&publications).Error; err != nil {
		t.Fatal(err)
	}
	failure := errors.New("publication insert rejected")
	store.Publish = func(tx *gorm.DB, node, endpoint, actor uint) error {
		if err := networkstore.EnqueueNodePublication(tx, node, endpoint, actor); err != nil {
			return err
		}
		return failure
	}
	if n, err := store.ExpireDueAccessCredentials(t.Context(), now, 200); !errors.Is(err, failure) || n != 0 {
		t.Fatal("publication failure escaped access transaction", n, err)
	}
	var after []model.ProtocolCredential
	var afterPublication []model.NodeConfigPublish
	if err := f.h.db.Order("id").Find(&after).Error; err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("partial credential revocation", err)
	}
	if err := f.h.db.Order("node_id").Find(&afterPublication).Error; err != nil || !reflect.DeepEqual(publications, afterPublication) {
		t.Fatal("partial publication request", err)
	}
	store.Publish = networkstore.EnqueueNodePublication
	// A real core renewal commits after the candidate scan, before its owner lock.
	renewal := f.create(t, sub.ID)
	wallet := fundingService(f)
	if _, err := wallet.Configure(t.Context(), 1, renewal.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	const hook = "test:renew-after-access-candidate"
	var injected atomic.Bool
	if err := f.h.db.Callback().Query().After("gorm:query").Register(hook, func(tx *gorm.DB) {
		if tx.Error != nil || tx.RowsAffected == 0 || tx.Statement.Table != "protocol_credentials" || !strings.Contains(tx.Statement.SQL.String(), "JOIN subscriptions") || !injected.CompareAndSwap(false, true) {
			return
		}
		if _, err := wallet.Pay(t.Context(), 1, renewal.ID); err != nil {
			tx.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.h.db.Callback().Query().Remove(hook) })
	if n, err := store.ExpireDueAccessCredentials(t.Context(), now, 200); err != nil || n != 0 || !injected.Load() {
		t.Fatal("stale expiry did not recheck actual renewal", n, err, injected.Load())
	}
	if err := f.h.db.Callback().Query().Remove(hook); err != nil {
		t.Fatal(err)
	}
	var renewed model.Subscription
	if err := f.h.db.First(&renewed, sub.ID).Error; err != nil || !renewed.EndAt.After(now) {
		t.Fatal("real core renewal did not extend the deadline", renewed.EndAt, err)
	}
	if err := f.h.db.Where("subscription_id=?", sub.ID).Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range after {
		if row.Status != "active" || !row.ExpiresAt.Equal(renewed.EndAt) {
			t.Fatal("old expiry candidate revoked renewed access", row.ID, row.Status)
		}
	}
}
