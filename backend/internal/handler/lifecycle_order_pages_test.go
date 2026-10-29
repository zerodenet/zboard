package handler

import (
	"context"
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
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const lifecyclePaidPagePopulation = 4097

type lifecyclePaidPageFixture struct {
	orderFixture
	root model.Order
	sub  model.Subscription
	now  time.Time
}

func lifecyclePaidPagesFixture(t *testing.T, f orderFixture) lifecyclePaidPageFixture {
	t.Helper()
	attachOrderPublishEndpoint(t, f)
	if err := f.h.db.Model(&f.planRecord).Update("is_renewable", false).Error; err != nil {
		t.Fatal(err)
	}
	fundTestWallet(t, f, 30000)
	created, err := f.h.services.OrderCreation.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, IdempotencyKey: "lifecycle-pages-paid-root"})
	if err != nil {
		t.Fatal(err)
	}
	payments := fundingService(f)
	if _, err := payments.Configure(t.Context(), 1, created.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := payments.Pay(t.Context(), 1, created.ID); err != nil {
		t.Fatal(err)
	}
	var root model.Order
	var sub model.Subscription
	if err := f.h.db.First(&root, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil || sub.Lifecycle != "fixed" {
		t.Fatal("fixture is not a fixed paid instance", sub, err)
	}
	// Only the root is a real wallet-funded checkout. The other rows are
	// explicit legacy paid metadata for lifecycle identity scale, not fabricated
	// verified cash snapshots or a claim of4096 gateway transactions.
	rows := make([]model.Order, lifecyclePaidPagePopulation-1)
	for i := range rows {
		rows[i] = model.Order{UserID: sub.UserID, SubscriptionID: sub.ID, PlanID: root.PlanID, PlanSKUID: root.PlanSKUID,
			TradeNo: fmt.Sprintf("lifecycle-historical-paid-%04d", i), OrderType: "renewal", Status: "paid",
			AmountCents: 17, PayableAmount: 17, PaidAmount: 17, Currency: root.Currency,
			PurchaseSnapshot: "uninterpreted historical purchase", EntitlementValueSnapshot: "uninterpreted historical value"}
	}
	if err := f.h.db.CreateInBatches(&rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	return lifecyclePaidPageFixture{f, root, sub, sub.EndAt.Add(time.Hour)}
}

func lifecyclePageMoney(t *testing.T, db *gorm.DB) any {
	t.Helper()
	var orders []struct {
		ID                                                                   uint
		AmountCents, DiscountAmount, PayableAmount, PaidAmount, RefundAmount int64
		Currency, PurchaseSnapshot, EntitlementValueSnapshot, ChangeSnapshot string
	}
	if err := db.Model(&model.Order{}).Order("id ASC").Find(&orders).Error; err != nil {
		t.Fatal(err)
	}
	var wallet []model.WalletAccount
	var entries []model.WalletEntry
	var receipts []model.PaymentReceipt
	if err := db.Order("user_id,currency").Find(&wallet).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&entries).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&receipts).Error; err != nil {
		t.Fatal(err)
	}
	return struct {
		Orders   any
		Wallet   any
		Entries  any
		Receipts any
	}{orders, wallet, entries, receipts}
}

func guardLifecycleOrderPages(t *testing.T, db *gorm.DB) func() {
	t.Helper()
	var pages atomic.Int64
	const name = "test:lifecycle-order-page-bound"
	if err := db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table != "orders" || tx.Error != nil || tx.RowsAffected <= 1 {
			return
		}
		limit, ok := tx.Statement.Clauses["LIMIT"].Expression.(clause.Limit)
		if !ok || limit.Limit == nil || *limit.Limit > 200 || tx.RowsAffected > 200 {
			tx.AddError(fmt.Errorf("lifecycle order query exceeded200 rows"))
			return
		}
		pages.Add(1)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Query().Remove(name) })
	return func() {
		db.Callback().Query().Remove(name)
		if pages.Load() < 21 {
			t.Fatal("4097 paid identities were not paged", pages.Load())
		}
	}
}

func lifecyclePagesService(f lifecyclePaidPageFixture) entitlementstore.CredentialExpiry {
	return entitlementstore.CredentialExpiry{DB: f.h.db, Cipher: f.h.credentialCipher,
		Publish: networkstore.EnqueueNodePublication, BeforeRetire: commercestore.CancelRetiredSubscriptionOrders,
		PrepareScope: func(ctx context.Context, subs []model.Subscription) (entitlementstore.CredentialExpiryScope, error) {
			return commercestore.PrepareLifecycleClosureScope(f.h.db.WithContext(ctx), subs)
		}}
}

func TestLifecyclePaidClosure4097PagesAndLateRollback(t *testing.T) {
	assertLifecyclePaidClosurePages(t, newOrderFixture(t))
}
func TestMySQLLifecyclePaidClosure4097PagesAndLateRollback(t *testing.T) {
	assertLifecyclePaidClosurePages(t, newMySQLBusinessOrderFixture(t))
}
func assertLifecyclePaidClosurePages(t *testing.T, base orderFixture) {
	f := lifecyclePaidPagesFixture(t, base)
	money := lifecyclePageMoney(t, f.h.db)
	var beforeCredentials []model.ProtocolCredential
	var beforeIntents []model.NodeConfigPublish
	if err := f.h.db.Order("id").Find(&beforeCredentials).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Order("node_id").Find(&beforeIntents).Error; err != nil {
		t.Fatal(err)
	}
	checkPages := guardLifecycleOrderPages(t, f.h.db)
	var writes atomic.Int64
	var injected atomic.Bool
	const hook = "test:lifecycle-last-page-sql-failure"
	if err := f.h.db.Callback().Update().After("gorm:update").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table != "orders" || tx.Error != nil || tx.RowsAffected == 0 {
			return
		}
		if tx.RowsAffected > 200 {
			tx.AddError(fmt.Errorf("lifecycle bulk write exceeded200 rows"))
			return
		}
		if writes.Add(1) == 21 {
			injected.Store(true)
			tx.AddError(tx.Exec("INSERT INTO zboard_test_missing_lifecycle_page_table(id) VALUES(1)").Error)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.h.db.Callback().Update().Remove(hook) })
	service := lifecyclePagesService(f)
	if out, err := service.ExpireDueCredentials(t.Context(), f.now, 20); err == nil || !injected.Load() || !strings.Contains(err.Error(), "zboard_test_missing_lifecycle_page_table") || len(out) != 0 {
		t.Fatal("late paid identity write did not roll back", out, err)
	}
	if err := f.h.db.Callback().Update().Remove(hook); err != nil {
		t.Fatal(err)
	}
	var rollback model.Subscription
	var credentials []model.ProtocolCredential
	var intents []model.NodeConfigPublish
	if err := f.h.db.First(&rollback, f.sub.ID).Error; err != nil || !reflect.DeepEqual(rollback, f.sub) {
		t.Fatal("partial lifecycle state", rollback, err)
	}
	if err := f.h.db.Order("id").Find(&credentials).Error; err != nil || !reflect.DeepEqual(credentials, beforeCredentials) {
		t.Fatal("partial revocation", err)
	}
	if err := f.h.db.Order("node_id").Find(&intents).Error; err != nil || !reflect.DeepEqual(intents, beforeIntents) {
		t.Fatal("partial publication", err)
	}
	var ended int64
	if err := f.h.db.Model(&model.Order{}).Where("subscription_id = ? AND subscription_ended_at IS NOT NULL", f.sub.ID).Count(&ended).Error; err != nil || ended != 0 {
		t.Fatal("earlier paid pages escaped rollback", ended, err)
	}
	if _, err := service.ExpireDueCredentials(t.Context(), f.now, 20); err != nil {
		t.Fatal("4097 paid identities prevented retirement", err)
	}
	checkPages()
	if !reflect.DeepEqual(money, lifecyclePageMoney(t, f.h.db)) {
		t.Fatal("lifecycle paging changed money or immutable purchase evidence")
	}
	if err := f.h.db.First(&rollback, f.sub.ID).Error; err != nil || rollback.Status != "ended" || rollback.EndedAt == nil || !rollback.EndedAt.Equal(f.sub.EndAt) || rollback.FlowUsed != f.sub.FlowUsed {
		t.Fatal("retirement did not preserve original period/use", rollback, err)
	}
	if err := f.h.db.Model(&model.Order{}).Where("subscription_id = ? AND subscription_ended_at = ? AND subscription_end_reason = ?", f.sub.ID, f.sub.EndAt, "expired").Count(&ended).Error; err != nil || ended != lifecyclePaidPagePopulation {
		t.Fatal("paid closure metadata incomplete", ended, err)
	}
	credentials = nil
	if err := f.h.db.Order("id").Find(&credentials).Error; err != nil || len(credentials) == 0 {
		t.Fatal(err)
	}
	for _, credential := range credentials {
		if credential.Status != "revoked" || credential.Secret != "" || credential.RevokedAt == nil || credential.CredentialID == "" || credential.PrincipalKey == "" {
			t.Fatal("credential identity lost or not retired", credential.ID)
		}
	}
	if out, err := service.ExpireDueCredentials(t.Context(), f.now, 20); err != nil || len(out) != 0 {
		t.Fatal("retirement replay changed result", out, err)
	}
	if due, err := service.HasDueCredentials(t.Context(), f.now); err != nil || due {
		t.Fatal("retirement did not converge", due, err)
	}
}

func TestLifecyclePaidClosureRechecksNewLastPageOrder(t *testing.T) {
	assertLifecycleClosureNewOrder(t, newOrderFixture(t))
}
func TestMySQLLifecyclePaidClosureRechecksNewLastPageOrder(t *testing.T) {
	assertLifecycleClosureNewOrder(t, newMySQLBusinessOrderFixture(t))
}
func assertLifecycleClosureNewOrder(t *testing.T, base orderFixture) {
	f := lifecyclePaidPagesFixture(t, base)
	sku := f.sku(t, f.planRecord.ID, 10, "addon")
	if err := f.h.db.Model(&sku).Updates(map[string]any{"entitlement_mode": "traffic_addon", "traffic_bytes": 10, "billing_unit": "once", "billing_mode": "one_time", "renewal_effect": "none"}).Error; err != nil {
		t.Fatal(err)
	}
	service := lifecyclePagesService(f)
	prepared, release := make(chan struct{}), make(chan struct{})
	original := service.PrepareScope
	var first atomic.Bool
	service.PrepareScope = func(ctx context.Context, subs []model.Subscription) (entitlementstore.CredentialExpiryScope, error) {
		scope, err := original(ctx, subs)
		if err == nil && first.CompareAndSwap(false, true) {
			close(prepared)
			select {
			case <-release:
			case <-ctx.Done():
				return scope, ctx.Err()
			}
		}
		return scope, err
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := service.ExpireDueCredentials(ctx, f.now, 20); done <- err }()
	select {
	case <-prepared:
	case err := <-done:
		t.Fatal("expiry did not prepare source pages", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	pending, err := f.h.services.OrderCreation.Create(ctx, 1, commerce.OrderCreateRequest{PlanSKUID: sku.ID, TargetSubscriptionID: f.sub.ID, IdempotencyKey: "lifecycle-real-checkout-between-source-and-owner"})
	if err != nil {
		close(release)
		<-done
		t.Fatal("real concurrent checkout failed", err)
	}
	close(release)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "membership changed") {
		t.Fatal("new high-ID order escaped Owner-after RR recheck", err)
	}
	var unchanged model.Subscription
	if err := f.h.db.First(&unchanged, f.sub.ID).Error; err != nil || !reflect.DeepEqual(unchanged, f.sub) {
		t.Fatal("stale scope partially ended rights", err)
	}
	var current model.Order
	if err := f.h.db.First(&current, pending.ID).Error; err != nil || current.Status != "pending" || current.TargetSubscriptionID == nil || *current.TargetSubscriptionID != f.sub.ID || current.SubscriptionEndedAt != nil {
		t.Fatal("stale scope rewrote concurrent checkout", err)
	}
	if _, err := service.ExpireDueCredentials(t.Context(), f.now, 20); err != nil {
		t.Fatal("fresh scope did not recover", err)
	}
	current = model.Order{}
	if err := f.h.db.First(&current, pending.ID).Error; err != nil || current.Status != "canceled" || current.TargetSubscriptionID == nil || *current.TargetSubscriptionID != f.sub.ID {
		t.Fatal("retry lost late-payment target/cancellation", err)
	}
}
