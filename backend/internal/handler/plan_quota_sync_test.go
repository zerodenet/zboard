package handler

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type planQuotaRaceMarker struct{}

// Hold the first authoritative subscription write until the other real request
// is demonstrably blocked: SQLite's single connection or an InnoDB row lock.
// Only business errors explicitly asserted by the caller are accepted.
func planQuotaInterleave(t *testing.T, db *gorm.DB, first, second func(context.Context) error) (error, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	const hook = "test:plan-quota-interleave"
	if err := db.Callback().Update().After("gorm:update").Register(hook, func(tx *gorm.DB) {
		statement := tx.Statement.SQL.String()
		if tx.Error != nil || tx.Statement.Context.Value(planQuotaRaceMarker{}) != "first" || tx.Statement.Table != "subscriptions" || (!strings.Contains(statement, "flow_total") && !strings.Contains(statement, "flow_used")) {
			return
		}
		enterOnce.Do(func() {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				tx.AddError(ctx.Err())
			}
		})
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Update().Remove(hook)
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	go func() { firstDone <- first(context.WithValue(ctx, planQuotaRaceMarker{}, "first")) }()
	select {
	case <-entered:
	case err := <-firstDone:
		t.Fatalf("first request did not reach its authority write: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	beforeWaits := pool.Stats().WaitCount
	go func() { secondDone <- second(context.WithValue(ctx, planQuotaRaceMarker{}, "second")) }()
	contended := false
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for !contended && ctx.Err() == nil {
		if db.Dialector.Name() == "sqlite" {
			contended = pool.Stats().WaitCount > beforeWaits
		} else {
			var waits int64
			err = db.WithContext(ctx).Raw("SELECT COUNT(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID = w.REQUESTING_ENGINE_LOCK_ID WHERE l.OBJECT_SCHEMA = DATABASE()").Scan(&waits).Error
			if err != nil {
				break
			}
			contended = waits > 0
		}
		if !contended {
			select {
			case <-ticker.C:
			case <-ctx.Done():
			}
		}
	}
	unblock()
	firstErr, secondErr := <-firstDone, <-secondDone
	if err != nil {
		t.Fatalf("contention probe failed: %v", err)
	}
	if !contended {
		t.Fatalf("requests never overlapped on their real database authority: %v", ctx.Err())
	}
	return firstErr, secondErr
}

func planQuotaRaceAdmin(t *testing.T, f fullReplacementFixture) uint {
	t.Helper()
	// A distinct administrator avoids accidentally serializing both operations
	// on the same user's lock before the plan/subscription locks are exercised.
	admin := model.User{Email: "plan-quota-race-admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
	if err := f.db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	return admin.ID
}

func TestPlanQuotaSyncPreservesUsageDatesAndFinancialHistory(t *testing.T) {
	assertPlanQuotaSync(t, newOrderFixture(t))
}
func TestMySQLPlanQuotaSyncPreservesUsageDatesAndFinancialHistory(t *testing.T) {
	assertPlanQuotaSync(t, newMySQLOrderFixture(t))
}
func assertPlanQuotaSync(t *testing.T, base orderFixture) {
	t.Helper()
	f := newFullReplacementFixture(t, base, false, 60, 3)
	db := f.db
	var original model.Subscription
	if err := db.First(&original, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	var oldOrder model.Order
	if err := db.First(&oldOrder, f.root.ID).Error; err != nil {
		t.Fatal(err)
	}
	var plan model.Plan
	if err := db.First(&plan, original.PlanID).Error; err != nil {
		t.Fatal(err)
	}
	quota := int64(200)
	updated, err := f.base.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, ExpectedRevision: &plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	var untouched model.Subscription
	if err := db.First(&untouched, original.ID).Error; err != nil || !reflect.DeepEqual(original, untouched) {
		t.Fatal("default plan edit mutated historical subscription", err)
	}
	request := commerce.OrderCreateRequest{PlanSKUID: f.target, TargetSubscriptionID: f.subscription, IdempotencyKey: "stale-before-plan-quota-sync"}
	quote, err := f.orders.Preview(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	updated, err = f.base.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, ExpectedRevision: &updated.Revision, SyncTrafficQuota: true})
	if err != nil {
		t.Fatal(err)
	}
	assertSyncState := func(wantQuota, wantRemaining int64) model.Subscription {
		t.Helper()
		var sub model.Subscription
		if err := db.First(&sub, original.ID).Error; err != nil {
			t.Fatal(err)
		}
		total, used := entitlements.CycleQuota(entitlements.Subscription(sub))
		if total != wantQuota || max(int64(0), total-used) != wantRemaining || sub.FlowUsed != original.FlowUsed || sub.CycleStartUsed != original.CycleStartUsed || !sub.StartAt.Equal(original.StartAt) || !sub.EndAt.Equal(original.EndAt) || !reflect.DeepEqual(sub.NextResetAt, original.NextResetAt) || sub.ResetPolicy != original.ResetPolicy || sub.RenewalPriceMinor != original.RenewalPriceMinor || sub.EntitlementEpoch != original.EntitlementEpoch {
			t.Fatalf("synchronization changed unrelated rights %+v", sub)
		}
		var order model.Order
		if err := db.First(&order, oldOrder.ID).Error; err != nil || !reflect.DeepEqual(oldOrder, order) {
			t.Fatal("synchronization rewrote immutable financial history", err)
		}
		return sub
	}
	assertSyncState(200, 140)
	request.QuoteFingerprint = quote.QuoteFingerprint
	if _, err := f.orders.Create(t.Context(), 1, request); err == nil {
		t.Fatal("stale replacement quote survived quota synchronization")
	}
	quota = 50
	updated, err = f.base.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, ExpectedRevision: &updated.Revision, SyncTrafficQuota: true})
	if err != nil {
		t.Fatal(err)
	}
	assertSyncState(50, 0)
	var credential model.ProtocolCredential
	if err := db.First(&credential, f.oldCredential.ID).Error; err != nil || credential.Status != "expired" {
		t.Fatalf("lower quota retained active credential %+v %v", credential, err)
	}
	var events int64
	if err := db.Model(&model.QuotaEvent{}).Where("subscription_id = ? AND event_type = ?", original.ID, "plan_quota_sync").Count(&events).Error; err != nil || events != 2 {
		t.Fatalf("quota facts missing %d %v", events, err)
	}
	var publication model.NodeConfigPublish
	if err := db.Where("node_id = ?", credential.NodeID).First(&publication).Error; err != nil {
		t.Fatal("quota sync lost durable delivery intent", err)
	}
	quota = 150
	if _, err := f.base.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, ExpectedRevision: &updated.Revision, SyncTrafficQuota: true}); err != nil {
		t.Fatal(err)
	}
	assertSyncState(150, 90)
	// The existing durable publication path reconciles credentials before render.
	if err := f.base.h.services.NodeCredentialReconciliation(f.base.h.credentialCipher, true).ReconcileNode(t.Context(), credential.NodeID); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&credential, credential.ID).Error; err != nil || credential.Status != "active" {
		t.Fatalf("quota restoration could not restore credential %+v %v", credential, err)
	}
}

func TestPlanQuotaSyncRollsBackPlanQuotaAndPublicationTogether(t *testing.T) {
	assertPlanQuotaSyncRollback(t, newOrderFixture(t))
}
func TestMySQLPlanQuotaSyncRollsBackPlanQuotaAndPublicationTogether(t *testing.T) {
	assertPlanQuotaSyncRollback(t, newMySQLOrderFixture(t))
}
func assertPlanQuotaSyncRollback(t *testing.T, base orderFixture) {
	t.Helper()
	f := newFullReplacementFixture(t, base, false, 60, 3)
	var before model.Subscription
	if err := f.db.First(&before, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	var plan model.Plan
	if err := f.db.First(&plan, before.PlanID).Error; err != nil {
		t.Fatal(err)
	}
	const hook = "plan-quota-sync-failure"
	if err := f.db.Callback().Create().Before("gorm:create").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table == "quota_events" {
			tx.AddError(errors.New("injected quota event failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	quota := int64(50)
	_, err := base.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, ExpectedRevision: &plan.Revision, SyncTrafficQuota: true})
	f.db.Callback().Create().Remove(hook)
	if err == nil {
		t.Fatal("quota event failure ignored")
	}
	var after model.Subscription
	var afterPlan model.Plan
	if err := f.db.First(&after, before.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&afterPlan, plan.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || plan.TrafficBytes != afterPlan.TrafficBytes || plan.Revision != afterPlan.Revision {
		t.Fatal("failed synchronization partially committed")
	}
	var credential model.ProtocolCredential
	if err := f.db.First(&credential, f.oldCredential.ID).Error; err != nil || credential.Status != f.oldCredential.Status {
		t.Fatal("failed synchronization expired credential", err)
	}
}

func TestPlanQuotaSyncRejectsOverflowAndDoesNotReviveTerminated(t *testing.T) {
	f := newOrderFixture(t)
	now := time.Now().UTC()
	ended := now.Add(-time.Hour)
	rows := []model.Subscription{{UserID: 1, PlanID: f.planRecord.ID, NodeGroupID: f.group.ID, Status: "active", StartAt: now, EndAt: now.AddDate(0, 1, 0), FlowTotal: math.MaxInt64, FlowUsed: math.MaxInt64 - 1, CycleStartUsed: math.MaxInt64 - 2, ResetQuotaBytes: 2}, {UserID: 1, PlanID: f.planRecord.ID, NodeGroupID: f.group.ID, Status: "canceled", StartAt: now, EndAt: now.AddDate(0, 1, 0), EndedAt: &ended, FlowTotal: 100, FlowUsed: 60}}
	if err := f.h.db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	quota := int64(200)
	_, err := f.h.services.PlanUpdate.Update(t.Context(), 1, f.planRecord.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, ExpectedRevision: &f.planRecord.Revision, SyncTrafficQuota: true})
	if err == nil {
		t.Fatal("overflow accepted")
	}
	var after model.Subscription
	if err := f.h.db.First(&after, rows[1].ID).Error; err != nil || after.FlowTotal != 100 || after.Status != "canceled" || after.EndedAt == nil {
		t.Fatal("canceled subscription revived", err)
	}
}

func TestPlanQuotaSyncSkipsTerminalAndElapsedButRestoresLegacyCycleMarker(t *testing.T) {
	f := newOrderFixture(t)
	now := time.Now().UTC()
	ended := now.Add(-time.Hour)
	rows := []model.Subscription{
		{UserID: 1, PlanID: f.planRecord.ID, NodeGroupID: f.group.ID, Status: "expired", StartAt: now, EndAt: now.AddDate(0, 1, 0), FlowTotal: 100, FlowUsed: 100, ResetQuotaBytes: 100, ResetPolicy: 1},
		{UserID: 1, PlanID: f.planRecord.ID, NodeGroupID: f.group.ID, Status: "canceled", StartAt: now, EndAt: now.AddDate(0, 1, 0), EndedAt: &ended, FlowTotal: 100, FlowUsed: 60},
		{UserID: 1, PlanID: f.planRecord.ID, NodeGroupID: f.group.ID, Status: "expired", StartAt: now, EndAt: ended, FlowTotal: 100, FlowUsed: 60},
		{UserID: 1, PlanID: f.planRecord.ID, NodeGroupID: f.group.ID, Status: "active", StartAt: now, EndAt: entitlements.PerpetualEnd, EndsOnQuotaExhaustion: true, FlowTotal: 100, FlowUsed: 100},
	}
	if err := f.h.db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for i := range rows {
		if err := f.h.db.First(&rows[i], rows[i].ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	quota := int64(200)
	if _, err := f.h.services.PlanUpdate.Update(t.Context(), 1, f.planRecord.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, ExpectedRevision: &f.planRecord.Revision, SyncTrafficQuota: true}); err != nil {
		t.Fatal(err)
	}
	for i, before := range rows {
		var after model.Subscription
		if err := f.h.db.First(&after, before.ID).Error; err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if after.FlowTotal != 200 || after.FlowUsed != 100 || after.Status != "active" || after.EndedAt != nil {
				t.Fatalf("legacy cycle marker not restored %+v", after)
			}
		} else if !reflect.DeepEqual(before, after) {
			t.Fatalf("terminal/elapsed subscription changed before=%+v after=%+v", before, after)
		}
	}
}

func TestPlanQuotaSyncBoundRejectsWholeTransaction(t *testing.T) {
	f := newOrderFixture(t)
	now := time.Now().UTC()
	rows := make([]model.Subscription, 1001)
	for i := range rows {
		rows[i] = model.Subscription{UserID: 1, PlanID: f.planRecord.ID, NodeGroupID: f.group.ID, Status: "active", StartAt: now, EndAt: now.AddDate(0, 1, 0), FlowTotal: 100, FlowUsed: 60, ResetQuotaBytes: 100}
	}
	if err := f.h.db.CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	quota := int64(200)
	_, err := f.h.services.PlanUpdate.Update(t.Context(), 1, f.planRecord.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, ExpectedRevision: &f.planRecord.Revision, SyncTrafficQuota: true})
	var invalid *commerce.ValidationError
	if !errors.As(err, &invalid) || invalid.Fields["sync_traffic_quota"] == "" {
		t.Fatalf("unbounded sync accepted %v", err)
	}
	var changed int64
	if err := f.h.db.Model(&model.Subscription{}).Where("plan_id = ? AND flow_total <> ?", f.planRecord.ID, 100).Count(&changed).Error; err != nil || changed != 0 {
		t.Fatal("bounded rejection partially synced", err)
	}
	var after model.Plan
	if err := f.h.db.First(&after, f.planRecord.ID).Error; err != nil || after.Revision != f.planRecord.Revision || after.TrafficBytes != f.planRecord.TrafficBytes {
		t.Fatal("bounded rejection modified plan", err)
	}
}

func TestPlanQuotaSyncPreservesPaidAddonByRefusingAmbiguousSync(t *testing.T) {
	assertPlanQuotaSyncAddonGuard(t, newOrderFixture(t))
}
func TestMySQLPlanQuotaSyncPreservesPaidAddonByRefusingAmbiguousSync(t *testing.T) {
	assertPlanQuotaSyncAddonGuard(t, newMySQLOrderFixture(t))
}
func assertPlanQuotaSyncAddonGuard(t *testing.T, base orderFixture) {
	t.Helper()
	f := newFullReplacementFixture(t, base, false, 60, 3)
	addon := base.sku(t, base.planRecord.ID, 500, "addon")
	if err := f.db.Model(&addon).Updates(map[string]any{"entitlement_mode": "traffic_addon", "traffic_bytes": 50, "billing_unit": "once", "billing_mode": "one_time", "renewal_effect": "none"}).Error; err != nil {
		t.Fatal(err)
	}
	order, err := f.orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: addon.ID, TargetSubscriptionID: f.subscription, IdempotencyKey: "addon-before-plan-sync"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Pay(t.Context(), 1, order.ID); err != nil {
		t.Fatal(err)
	}
	var before model.Subscription
	if err := f.db.First(&before, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	if before.FlowTotal-before.CycleStartUsed != 150 || before.ResetQuotaBytes != 100 {
		t.Fatalf("actual paid addon fixture did not grant distinct quota %+v", before)
	}
	var paid model.Order
	if err := f.db.First(&paid, order.ID).Error; err != nil || paid.PaidAmount != 500 {
		t.Fatal("addon lacks actual payment", err)
	}
	var plan model.Plan
	if err := f.db.First(&plan, before.PlanID).Error; err != nil {
		t.Fatal(err)
	}
	quota := int64(200)
	_, err = base.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, ExpectedRevision: &plan.Revision, SyncTrafficQuota: true})
	var invalid *commerce.ValidationError
	if !errors.As(err, &invalid) || invalid.Fields["sync_traffic_quota"] == "" {
		t.Fatalf("mixed paid rights overwritten: %v", err)
	}
	var after model.Subscription
	var afterPlan model.Plan
	var afterOrder model.Order
	if err := f.db.First(&after, before.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&afterPlan, plan.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&afterOrder, paid.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(paid, afterOrder) || afterPlan.Revision != plan.Revision || afterPlan.TrafficBytes != plan.TrafficBytes {
		t.Fatal("ambiguous synchronization lost paid addon or partially changed plan")
	}
}

func TestPlanQuotaSyncConcurrentWalletRenewal(t *testing.T) {
	assertPlanQuotaSyncRenewalRace(t, func(t *testing.T) orderFixture { return newOrderFixture(t) })
}
func TestMySQLPlanQuotaSyncConcurrentWalletRenewal(t *testing.T) {
	assertPlanQuotaSyncRenewalRace(t, newMySQLOrderFixture)
}
func assertPlanQuotaSyncRenewalRace(t *testing.T, fixture func(*testing.T) orderFixture) {
	t.Helper()
	for _, syncFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "sync_first", false: "renewal_first"}[syncFirst], func(t *testing.T) {
			f := newFullReplacementFixture(t, fixture(t), false, 60, 3)
			admin := planQuotaRaceAdmin(t, f)
			var before model.Subscription
			var plan model.Plan
			if err := f.db.First(&before, f.subscription).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.db.First(&plan, before.PlanID).Error; err != nil {
				t.Fatal(err)
			}
			request := commerce.OrderCreateRequest{PlanSKUID: f.base.skuRecord.ID, TargetSubscriptionID: f.subscription, IdempotencyKey: "renewal-racing-plan-quota"}
			quote, err := f.orders.Preview(t.Context(), 1, request)
			if err != nil {
				t.Fatal(err)
			}
			request.QuoteFingerprint = quote.QuoteFingerprint
			order, err := f.orders.Create(t.Context(), 1, request)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
				t.Fatal(err)
			}
			quota := int64(200)
			synchronize := func(ctx context.Context) error {
				_, err := f.base.h.services.PlanUpdate.Update(ctx, admin, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, ExpectedRevision: &plan.Revision, SyncTrafficQuota: true})
				return err
			}
			pay := func(ctx context.Context) error { _, err := f.payments.Pay(ctx, 1, order.ID); return err }
			var syncErr, payErr error
			if syncFirst {
				syncErr, payErr = planQuotaInterleave(t, f.db, synchronize, pay)
			} else {
				payErr, syncErr = planQuotaInterleave(t, f.db, pay, synchronize)
			}
			if syncErr != nil {
				t.Fatalf("concurrent plan synchronization failed: %v", syncErr)
			}
			var after model.Subscription
			var wallet model.WalletAccount
			var persisted model.Order
			if err := f.db.First(&after, before.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.db.First(&persisted, order.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil {
				t.Fatal(err)
			}
			if after.FlowUsed != before.FlowUsed || after.FlowTotal != before.CycleStartUsed+200 || after.ResetQuotaBytes != 200 || !reflect.DeepEqual(after.NextResetAt, before.NextResetAt) || after.EntitlementEpoch != before.EntitlementEpoch {
				t.Fatalf("renewal/sync race lost quota or identity %+v", after)
			}
			var renewalFacts int64
			if err := f.db.Model(&model.QuotaEvent{}).Where("subscription_id = ? AND event_type = ?", before.ID, "renewal").Count(&renewalFacts).Error; err != nil {
				t.Fatal(err)
			}
			if syncFirst {
				var stale *commerce.ValidationError
				if !errors.As(payErr, &stale) || stale.Fields["target_subscription_id"] == "" {
					t.Fatalf("stale renewal payment did not reject its changed target: %v", payErr)
				}
				if !after.EndAt.Equal(before.EndAt) || persisted.Status != "pending" || persisted.PaidAmount != 0 || renewalFacts != 0 || wallet.BalanceMinor != 18000 || wallet.ReservedMinor != 12000 {
					t.Fatalf("stale renewal committed financial/term changes sub=%+v order=%+v wallet=%+v facts=%d", after, persisted, wallet, renewalFacts)
				}
				if _, err := f.payments.Pay(t.Context(), 1, order.ID); !errors.As(err, &stale) {
					t.Fatalf("stale retry fulfilled renewal: %v", err)
				}
			} else {
				if payErr != nil {
					t.Fatalf("valid renewal failed against later synchronization: %v", payErr)
				}
				if !after.EndAt.Equal(before.EndAt.AddDate(1, 0, 0)) || persisted.Status != "paid" || persisted.PaidAmount != 12000 || renewalFacts != 1 || wallet.BalanceMinor != 6000 || wallet.ReservedMinor != 0 {
					t.Fatalf("valid renewal/sync did not conserve value sub=%+v wallet=%+v facts=%d", after, wallet, renewalFacts)
				}
				if _, err := f.payments.Pay(t.Context(), 1, order.ID); err != nil {
					t.Fatal(err)
				}
				if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 6000 || wallet.ReservedMinor != 0 {
					t.Fatal("renewal retry charged twice", err)
				}
			}
		})
	}
}

func TestPlanQuotaSyncConcurrentOriginalMetering(t *testing.T) {
	assertPlanQuotaSyncMeteringRace(t, func(t *testing.T) orderFixture { return newOrderFixture(t) })
}
func TestMySQLPlanQuotaSyncConcurrentOriginalMetering(t *testing.T) {
	assertPlanQuotaSyncMeteringRace(t, newMySQLOrderFixture)
}
func assertPlanQuotaSyncMeteringRace(t *testing.T, fixture func(*testing.T) orderFixture) {
	t.Helper()
	for _, syncFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "sync_first", false: "metering_first"}[syncFirst], func(t *testing.T) {
			f := newFullReplacementFixture(t, fixture(t), false, 60, 3)
			admin := planQuotaRaceAdmin(t, f)
			var before model.Subscription
			var plan model.Plan
			if err := f.db.First(&before, f.subscription).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.db.First(&plan, before.PlanID).Error; err != nil {
				t.Fatal(err)
			}
			stale := confirmedReplacement(t, f, "replacement-before-quota-meter-race")
			if _, err := f.payments.Configure(t.Context(), 1, stale.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
				t.Fatal(err)
			}
			accounting := metering.CompletionAccounting{Repository: meteringstore.CompletionAccounting{DB: f.db, Cipher: f.base.h.credentialCipher, EnqueuePublications: networkstore.EnqueueSubscriptionPublications}}
			event := metering.CompletedFlow{NodeID: f.oldCredential.NodeID, CoreInstanceID: "plan-quota-race", EventID: "plan-quota-race-25", EventType: "flow.completed", Sequence: 1, FlowID: "plan-quota-race-flow", PrincipalKey: f.oldCredential.PrincipalKey, BytesDown: 25, OccurredAt: *f.at}
			meter := func(ctx context.Context) error { _, _, err := accounting.Complete(ctx, event); return err }
			quota := int64(200)
			synchronize := func(ctx context.Context) error {
				_, err := f.base.h.services.PlanUpdate.Update(ctx, admin, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, ExpectedRevision: &plan.Revision, SyncTrafficQuota: true})
				return err
			}
			var syncErr, meterErr error
			if syncFirst {
				syncErr, meterErr = planQuotaInterleave(t, f.db, synchronize, meter)
			} else {
				meterErr, syncErr = planQuotaInterleave(t, f.db, meter, synchronize)
			}
			if syncErr != nil || meterErr != nil {
				t.Fatalf("sync/metering race failed: sync=%v meter=%v", syncErr, meterErr)
			}
			if _, _, err := accounting.Complete(t.Context(), event); err != nil {
				t.Fatal(err)
			}
			var after model.Subscription
			if err := f.db.First(&after, before.ID).Error; err != nil {
				t.Fatal(err)
			}
			if after.FlowUsed != before.FlowUsed+25 || after.FlowTotal != before.CycleStartUsed+200 || after.CycleStartUsed != before.CycleStartUsed || !after.EndAt.Equal(before.EndAt) || !reflect.DeepEqual(after.NextResetAt, before.NextResetAt) {
				t.Fatalf("sync lost original accounting or original cycle %+v", after)
			}
			var facts []model.TrafficRecord
			if err := f.db.Where("node_id = ? AND report_id = ?", event.NodeID, event.EventID).Find(&facts).Error; err != nil || len(facts) != 1 || facts[0].RawBytes != 25 || facts[0].UsedBytes != 25 {
				t.Fatalf("duplicate/lost metering ledger %+v %v", facts, err)
			}
			var invalid *commerce.ValidationError
			if _, err := f.payments.Pay(t.Context(), 1, stale.ID); !errors.As(err, &invalid) {
				t.Fatalf("stale replacement fulfilled after quota/usage changed: %v", err)
			}
			var order model.Order
			var wallet model.WalletAccount
			if err := f.db.First(&order, stale.ID).Error; err != nil || order.Status != "pending" || order.PaidAmount != 0 {
				t.Fatal("stale replacement gained paid rights", err)
			}
			if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 18000 || wallet.ReservedMinor != 0 {
				t.Fatal("stale replacement converted or spent value", err)
			}
		})
	}
}
