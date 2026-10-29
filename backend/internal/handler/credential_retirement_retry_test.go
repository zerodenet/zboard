package handler

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func retirementPendingMetadata(t *testing.T, f orderFixture, sub model.Subscription, count int) []model.Order {
	t.Helper()
	rows := make([]model.Order, count)
	for n := range rows {
		target := sub.ID
		rows[n] = model.Order{UserID: sub.UserID, TargetSubscriptionID: &target, PlanID: sub.PlanID, PlanSKUID: sub.PlanSKUID, TradeNo: fmt.Sprintf("retirement-held-%d-%04d", sub.ID, n), OrderType: "renewal", Status: "pending", Currency: "CNY"}
	}
	if err := f.h.db.CreateInBatches(&rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestCredentialRetirementBudgetDefersOnlyOversizedSubscription(t *testing.T) {
	assertRetirementBudgetFairness(t, newOrderFixture(t))
}
func TestMySQLCredentialRetirementBudgetDefersOnlyOversizedSubscription(t *testing.T) {
	assertRetirementBudgetFairness(t, newMySQLBusinessOrderFixture(t))
}
func assertRetirementBudgetFairness(t *testing.T, f orderFixture) {
	attachOrderPublishEndpoint(t, f)
	if err := f.h.db.Model(&f.planRecord).Update("is_renewable", false).Error; err != nil {
		t.Fatal(err)
	}
	fundTestWallet(t, f, 30000)
	bad := accessExpiryPaidRoot(t, f, "retirement-budget-bad")
	healthy := accessExpiryPaidRoot(t, f, "retirement-budget-healthy")
	// One real wallet reservation plus1000 explicit unpaid legacy rows exercise
	// the exact mutable-order bound without manufacturing verified payment facts.
	sku := f.sku(t, f.planRecord.ID, 10, skuOperationAddon)
	if err := f.h.db.Model(&sku).Updates(map[string]any{"entitlement_mode": "traffic_addon", "traffic_bytes": 10, "billing_unit": "once", "billing_mode": "one_time", "renewal_effect": "none"}).Error; err != nil {
		t.Fatal(err)
	}
	reserved, err := f.h.services.OrderCreation.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: sku.ID, TargetSubscriptionID: bad.ID, IdempotencyKey: "retirement-held-wallet"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fundingService(f).Configure(t.Context(), 1, reserved.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	retirementPendingMetadata(t, f, bad, 1000)
	var beforeOrders []model.Order
	var beforeFunding []model.OrderFunding
	if err := f.h.db.Where("subscription_id=? OR target_subscription_id=?", bad.ID, bad.ID).Order("id").Find(&beforeOrders).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Order("order_id").Find(&beforeFunding).Error; err != nil {
		t.Fatal(err)
	}
	money := lifecyclePageMoney(t, f.h.db)
	now := healthy.EndAt.Add(time.Hour)
	store := accessExpiryStore(f)
	out, err := store.ExpireDueCredentials(t.Context(), now, 200)
	if !errors.Is(err, commerce.ErrFundingConflict) || len(out) == 0 {
		t.Fatal("real budget failure prevented healthy closure", out, err)
	}
	for _, credential := range out {
		if credential.SubscriptionID == bad.ID {
			t.Fatal("rolled-back credential falsely reported expired", credential)
		}
	}
	var held, closed model.Subscription
	if err := f.h.db.First(&held, bad.ID).Error; err != nil {
		t.Fatal(err)
	}
	expected := bad
	expected.RetirementRetryAt = held.RetirementRetryAt
	if held.RetirementRetryAt == nil || !held.RetirementRetryAt.After(now) || !reflect.DeepEqual(held, expected) {
		t.Fatal("budget hold changed business rights or was not durable", held, expected)
	}
	if err := f.h.db.First(&closed, healthy.ID).Error; err != nil || closed.Status != "ended" || closed.RetirementRetryAt != nil {
		t.Fatal("higher subscription was starved", closed, err)
	}
	var orders []model.Order
	var funding []model.OrderFunding
	if err := f.h.db.Where("subscription_id=? OR target_subscription_id=?", bad.ID, bad.ID).Order("id").Find(&orders).Error; err != nil || !reflect.DeepEqual(beforeOrders, orders) {
		t.Fatal("failed sub leaked cancellation/ended metadata", err)
	}
	if err := f.h.db.Order("order_id").Find(&funding).Error; err != nil || !reflect.DeepEqual(beforeFunding, funding) {
		t.Fatal("failed sub leaked wallet release", err)
	}
	if !reflect.DeepEqual(money, lifecyclePageMoney(t, f.h.db)) {
		t.Fatal("budget handling changed money or purchase evidence")
	}
	var audit model.AuditLog
	if err := f.h.db.Where("action=? AND target=?", "subscription.retirement_deferred", fmt.Sprintf("subscription:%d", bad.ID)).First(&audit).Error; err != nil || !strings.Contains(audit.Detail, "reason=mutable_order_budget") {
		t.Fatal("hold reason not auditable", audit, err)
	}
	// Future financial scheduling must never hide still-active expired access.
	if due, err := store.HasDueCredentials(t.Context(), now); err != nil || !due {
		t.Fatal("retry hid unrevoked expired access", due, err)
	}
	if n, err := store.ExpireDueAccessCredentials(t.Context(), now, 200); err != nil || n != 1 {
		t.Fatal("independent access stop failed", n, err)
	}
	if due, err := store.HasDueCredentials(t.Context(), now); err != nil || due {
		t.Fatal("future retry did not avoid immediate hot loop", due, err)
	}
	if _, err := f.h.services.OrderCancellation.Owned(t.Context(), 1, reserved.ID); err != nil {
		t.Fatal("real cancellation failed", err)
	}
	retry := held.RetirementRetryAt.Add(time.Second)
	settledMoney := lifecyclePageMoney(t, f.h.db)
	if _, err := store.ExpireDueCredentials(t.Context(), retry, 200); err != nil {
		t.Fatal("retry could not retire after pressure was resolved", err)
	}
	held = model.Subscription{}
	if err := f.h.db.First(&held, bad.ID).Error; err != nil || held.Status != "ended" || held.RetirementRetryAt != nil {
		t.Fatal("retry did not clear operational hold", held, err)
	}
	if !reflect.DeepEqual(settledMoney, lifecyclePageMoney(t, f.h.db)) {
		t.Fatal("retirement double-released consumed/canceled funds")
	}
	if out, err := store.ExpireDueCredentials(t.Context(), retry, 200); err != nil || len(out) != 0 {
		t.Fatal("retirement replay was not idempotent", out, err)
	}
}

func TestCredentialRetirementFreshRowsPrecedeTwoHundredDueRetries(t *testing.T) {
	assertRetirementFreshPriority(t, newOrderFixture(t))
}
func TestMySQLCredentialRetirementFreshRowsPrecedeTwoHundredDueRetries(t *testing.T) {
	assertRetirementFreshPriority(t, newMySQLBusinessOrderFixture(t))
}
func assertRetirementFreshPriority(t *testing.T, f orderFixture) {
	attachOrderPublishEndpoint(t, f)
	if err := f.h.db.Model(&f.planRecord).Update("is_renewable", false).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(time.Hour)
	past := now.Add(-time.Minute)
	// Retry rows are explicit historical subscription metadata, not paid orders.
	rows := make([]model.Subscription, 200)
	for n := range rows {
		rows[n] = model.Subscription{UserID: 1, PlanID: f.planRecord.ID, PlanSKUID: f.skuRecord.ID, Lifecycle: "fixed", Status: "active", StartAt: now.Add(-2 * time.Hour), EndAt: now.Add(-time.Hour), FlowTotal: 100, RetirementRetryAt: &past}
	}
	if err := f.h.db.CreateInBatches(&rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	fundTestWallet(t, f, 30000)
	healthy := accessExpiryPaidRoot(t, f, "retirement-fresh-after-200")
	now = healthy.EndAt.Add(time.Hour)
	// Real payment may perform the existing user-scoped expiry before granting
	// the new subscription. Capture that committed state, not the insertion-only
	// fixture: this test permits only retry scheduling to change from now on.
	ids := make([]uint, len(rows))
	for n := range rows {
		ids[n] = rows[n].ID
	}
	var baseline []model.Subscription
	if err := f.h.db.Where("id IN ?", ids).Order("id").Find(&baseline).Error; err != nil || len(baseline) != len(rows) {
		t.Fatal("capture committed retry cohort", len(baseline), err)
	}
	money := lifecyclePageMoney(t, f.h.db)
	expectedRetries := map[uint]time.Time{}
	assertRetryCohort := func(changed ...uint) {
		t.Helper()
		held := make(map[uint]bool, len(changed))
		for _, id := range changed {
			held[id] = true
		}
		var actual []model.Subscription
		if err := f.h.db.Where("id IN ?", ids).Order("id").Find(&actual).Error; err != nil || len(actual) != len(baseline) {
			t.Fatal("read retry cohort", len(actual), err)
		}
		for n, row := range actual {
			want := baseline[n]
			if held[row.ID] {
				if row.RetirementRetryAt == nil || !row.RetirementRetryAt.After(now) {
					t.Fatal("identity budget not durably held", row)
				}
				retry, seen := expectedRetries[row.ID]
				if !seen {
					retry = *row.RetirementRetryAt
					expectedRetries[row.ID] = retry
				}
				want.RetirementRetryAt = &retry
			}
			if !reflect.DeepEqual(row, want) {
				t.Fatal("retry changed committed rights, history or another row", row, want)
			}
		}
		if !reflect.DeepEqual(money, lifecyclePageMoney(t, f.h.db)) {
			t.Fatal("retry scheduling changed money or purchase evidence")
		}
	}
	store := accessExpiryStore(f)
	original := store.PrepareScope
	store.PrepareScope = func(ctx context.Context, subs []model.Subscription) (entitlementstore.CredentialExpiryScope, error) {
		if len(subs) != 1 || subs[0].ID != healthy.ID {
			return entitlementstore.CredentialExpiryScope{}, entitlements.ErrCredentialExpiryScopeBudget
		}
		return original(ctx, subs)
	}
	if _, err := store.ExpireDueCredentials(t.Context(), now, 1); err != nil {
		t.Fatal("fresh high ID starved behind200 due retry rows", err)
	}
	var closed model.Subscription
	if err := f.h.db.First(&closed, healthy.ID).Error; err != nil || closed.Status != "ended" {
		t.Fatal("fresh row was not closed", closed, err)
	}
	assertRetryCohort()
	// At the next turn a precise identity budget defers the oldest retry, then
	// advances durably to the next one even across repository reconstruction.
	if _, err := store.ExpireDueCredentials(t.Context(), now, 1); !errors.Is(err, entitlements.ErrCredentialExpiryScopeBudget) {
		t.Fatal("identity budget not preserved", err)
	}
	assertRetryCohort(rows[0].ID)
	restarted := accessExpiryStore(f)
	restarted.PrepareScope = store.PrepareScope
	if _, err := restarted.ExpireDueCredentials(t.Context(), now, 1); !errors.Is(err, entitlements.ErrCredentialExpiryScopeBudget) {
		t.Fatal("restarted retry missing budget receipt", err)
	}
	assertRetryCohort(rows[0].ID, rows[1].ID)
}

func TestCredentialRetirementBudgetHoldRechecksRealRenewal(t *testing.T) {
	assertRetirementHoldRenewal(t, newOrderFixture(t))
}
func TestMySQLCredentialRetirementBudgetHoldRechecksRealRenewal(t *testing.T) {
	assertRetirementHoldRenewal(t, newMySQLBusinessOrderFixture(t))
}
func assertRetirementHoldRenewal(t *testing.T, f orderFixture) {
	attachOrderPublishEndpoint(t, f)
	fundTestWallet(t, f, 30000)
	sub := accessExpiryPaidRoot(t, f, "retirement-stale-hold-root")
	renewal := f.create(t, sub.ID)
	wallet := fundingService(f)
	if _, err := wallet.Configure(t.Context(), 1, renewal.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	now := sub.EndAt.Add(time.Hour)
	store := accessExpiryStore(f)
	store.PrepareScope = func(context.Context, []model.Subscription) (entitlementstore.CredentialExpiryScope, error) {
		if _, err := wallet.Pay(t.Context(), 1, renewal.ID); err != nil {
			return entitlementstore.CredentialExpiryScope{}, err
		}
		return entitlementstore.CredentialExpiryScope{}, entitlements.ErrCredentialExpiryScopeBudget
	}
	if out, err := store.ExpireDueCredentials(t.Context(), now, 1); !errors.Is(err, entitlements.ErrCredentialExpiryScopeBudget) || len(out) != 0 {
		t.Fatal("missing original budget receipt", out, err)
	}
	var renewed model.Subscription
	if err := f.h.db.First(&renewed, sub.ID).Error; err != nil || !renewed.EndAt.After(now) || renewed.RetirementRetryAt != nil || renewed.Status != "active" {
		t.Fatal("stale budget hold overrode actual core renewal", renewed, err)
	}
	var audits int64
	if err := f.h.db.Model(&model.AuditLog{}).Where("action=? AND target=?", "subscription.retirement_deferred", fmt.Sprintf("subscription:%d", sub.ID)).Count(&audits).Error; err != nil || audits != 0 {
		t.Fatal("stale hold audit claimed deferral", audits, err)
	}
}

func TestCredentialRetirementOrdinaryFundingFailureRollsBackAndDoesNotDefer(t *testing.T) {
	assertRetirementOrdinaryFailure(t, newOrderFixture(t))
}
func TestMySQLCredentialRetirementOrdinaryFundingFailureRollsBackAndDoesNotDefer(t *testing.T) {
	assertRetirementOrdinaryFailure(t, newMySQLBusinessOrderFixture(t))
}
func assertRetirementOrdinaryFailure(t *testing.T, f orderFixture) {
	attachOrderPublishEndpoint(t, f)
	fundTestWallet(t, f, 30000)
	sub := accessExpiryPaidRoot(t, f, "retirement-ordinary-failure-root")
	pending := f.create(t, sub.ID)
	if _, err := fundingService(f).Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	money := lifecyclePageMoney(t, f.h.db)
	var beforeOrders []model.Order
	var beforeFunding []model.OrderFunding
	var beforeCredentials []model.ProtocolCredential
	var beforePublication []model.NodeConfigPublish
	if err := f.h.db.Order("id").Find(&beforeOrders).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Order("order_id").Find(&beforeFunding).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Order("id").Find(&beforeCredentials).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Order("node_id").Find(&beforePublication).Error; err != nil {
		t.Fatal(err)
	}
	store := accessExpiryStore(f)
	// Force cleanup after the renewable grace period. The real sole wallet
	// release/cancel path runs first, then an ordinary funding failure occurs.
	failure := commerce.ErrFundingConflict
	store.BeforeRetire = func(tx *gorm.DB, sub model.Subscription, now time.Time) error {
		if err := commercestore.CancelRetiredSubscriptionOrders(tx, sub, now); err != nil {
			return err
		}
		return failure
	}
	now := sub.EndAt.Add(entitlements.RenewalGracePeriod + time.Hour)
	if out, err := store.ExpireDueCredentials(t.Context(), now, 200); !errors.Is(err, failure) || len(out) != 0 {
		t.Fatal("ordinary funding failure was hidden or output escaped rollback", out, err)
	}
	var current model.Subscription
	if err := f.h.db.First(&current, sub.ID).Error; err != nil || !reflect.DeepEqual(current, sub) {
		t.Fatal("ordinary failure changed rights or scheduled retry", current, err)
	}
	var orders []model.Order
	var funds []model.OrderFunding
	var credentials []model.ProtocolCredential
	var publication []model.NodeConfigPublish
	if err := f.h.db.Order("id").Find(&orders).Error; err != nil || !reflect.DeepEqual(beforeOrders, orders) {
		t.Fatal("partial order cancellation", err)
	}
	if err := f.h.db.Order("order_id").Find(&funds).Error; err != nil || !reflect.DeepEqual(beforeFunding, funds) {
		t.Fatal("partial funding release", err)
	}
	if err := f.h.db.Order("id").Find(&credentials).Error; err != nil || !reflect.DeepEqual(beforeCredentials, credentials) {
		t.Fatal("partial credential revoke", err)
	}
	if err := f.h.db.Order("node_id").Find(&publication).Error; err != nil || !reflect.DeepEqual(beforePublication, publication) {
		t.Fatal("partial publication", err)
	}
	if !reflect.DeepEqual(money, lifecyclePageMoney(t, f.h.db)) {
		t.Fatal("ordinary failure escaped financial transaction")
	}
	var holds int64
	if err := f.h.db.Model(&model.AuditLog{}).Where("action=?", "subscription.retirement_deferred").Count(&holds).Error; err != nil || holds != 0 {
		t.Fatal("ordinary funding conflict was misclassified as budget", holds, err)
	}
}

func TestCredentialRetirementHoldAuditFailureRollsBackSchedule(t *testing.T) {
	assertRetirementHoldAuditFailure(t, newOrderFixture(t))
}
func TestMySQLCredentialRetirementHoldAuditFailureRollsBackSchedule(t *testing.T) {
	assertRetirementHoldAuditFailure(t, newMySQLBusinessOrderFixture(t))
}
func assertRetirementHoldAuditFailure(t *testing.T, f orderFixture) {
	attachOrderPublishEndpoint(t, f)
	fundTestWallet(t, f, 30000)
	sub := accessExpiryPaidRoot(t, f, "retirement-hold-audit-failure-root")
	store := accessExpiryStore(f)
	store.PrepareScope = func(context.Context, []model.Subscription) (entitlementstore.CredentialExpiryScope, error) {
		return entitlementstore.CredentialExpiryScope{}, entitlements.ErrCredentialExpiryScopeBudget
	}
	const hook = "test:retirement-hold-audit-failure"
	failure := errors.New("audit insert failed")
	if err := f.h.db.Callback().Create().Before("gorm:create").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_logs" {
			if log, ok := tx.Statement.Dest.(*model.AuditLog); ok && log.Action == "subscription.retirement_deferred" {
				tx.AddError(failure)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.h.db.Callback().Create().Remove(hook) })
	if out, err := store.ExpireDueCredentials(t.Context(), sub.EndAt.Add(time.Hour), 1); !errors.Is(err, failure) || len(out) != 0 {
		t.Fatal("hold persistence failure was hidden", out, err)
	}
	var after model.Subscription
	if err := f.h.db.First(&after, sub.ID).Error; err != nil || !reflect.DeepEqual(after, sub) {
		t.Fatal("unaudited retry delay committed", after, err)
	}
}
