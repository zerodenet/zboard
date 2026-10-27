package handler

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func planBaseSyncAddon(t *testing.T, f orderFixture, sub uint, quota, paid int64) model.Order {
	t.Helper()
	sku := f.sku(t, f.planRecord.ID, paid, "addon")
	if err := f.h.db.Model(&sku).Updates(map[string]any{"entitlement_mode": "traffic_addon", "traffic_bytes": quota, "billing_unit": "once", "billing_mode": "one_time", "renewal_effect": "none"}).Error; err != nil {
		t.Fatal(err)
	}
	order, err := f.h.services.OrderAssignment.Assign(t.Context(), 1, commerce.OrderAssignmentRequest{UserID: 1, PlanSKUID: sku.ID, TargetSubscriptionID: sub, PayableAmount: &paid, RequestID: uuid.NewString(), Note: "base-only synchronization actual paid add-on"})
	if err != nil {
		t.Fatal(err)
	}
	payments := fundingService(f)
	if _, err := payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := payments.Pay(t.Context(), 1, order.ID); err != nil {
		t.Fatal(err)
	}
	var actual model.Order
	if err := f.h.db.First(&actual, order.ID).Error; err != nil {
		t.Fatal(err)
	}
	var proof commerce.EntitlementValueSnapshot
	if err := json.Unmarshal([]byte(actual.EntitlementValueSnapshot), &proof); err != nil || proof.Mode != "perpetual_quota_addition" || !proof.PaymentVerified || proof.ActualPaidMinor != paid {
		t.Fatal("fixture did not capture actual separate paid quota", err)
	}
	return actual
}

func TestPlanBaseQuotaSyncOnlyChangesOriginalBase(t *testing.T) {
	assertPlanBaseQuotaSync(t, newOrderFixture(t))
}
func TestMySQLPlanBaseQuotaSyncOnlyChangesOriginalBase(t *testing.T) {
	assertPlanBaseQuotaSync(t, newMySQLBusinessOrderFixture(t))
}
func assertPlanBaseQuotaSync(t *testing.T, base orderFixture) {
	f, root := perpetualValueFixtureWithBase(t, base, 60)
	first := planBaseSyncAddon(t, f, root.SubscriptionID, 100, 6000)
	second := planBaseSyncAddon(t, f, root.SubscriptionID, 50, 1500)
	beforeMoney := planSyncFinancialSnapshot(t, f.h.db)
	var before model.Subscription
	var plan model.Plan
	if err := f.h.db.First(&before, root.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&plan, before.PlanID).Error; err != nil {
		t.Fatal(err)
	}
	quota := int64(200)
	updated, err := f.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	var after model.Subscription
	if err := f.h.db.First(&after, before.ID).Error; err != nil {
		t.Fatal(err)
	}
	want := before
	want.FlowTotal, want.ResetQuotaBytes, want.UpdatedAt = 350, 200, after.UpdatedAt
	if !reflect.DeepEqual(after, want) {
		t.Fatalf("base-only sync changed paid add-ons, used, identity or dates got=%+v want=%+v", after, want)
	}
	var rows []model.QuotaEvent
	if err := f.h.db.Where("subscription_id = ? AND event_type = ?", after.ID, "plan_quota_sync").Find(&rows).Error; err != nil || len(rows) != 1 {
		t.Fatal("missing or duplicate non-payment event", err)
	}
	detail, err := commerce.DecodePerpetualBaseQuotaSyncDetail([]byte(rows[0].Detail))
	if err != nil || detail.BaseOrderID != root.ID || detail.BaseOrderID == first.ID || detail.BaseOrderID == second.ID || detail.OldBaseQuota != 100 || detail.NewBaseQuota != 200 || detail.UsedAtSync != 60 || detail.EntitlementEpoch != after.EntitlementEpoch || rows[0].EntitlementEpoch != after.EntitlementEpoch || detail.PlanRevision != updated.Revision || detail.SourceFingerprint == "" {
		t.Fatalf("newest purchase was mistaken for base or event unbound: %+v %v", detail, err)
	}
	quote, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, after.ID)
	// Base: 10000 * 140/200 = 7000. Paid add-ons retain 6000 + 1500.
	if err != nil || quote.TotalMinor != 14500 {
		t.Fatalf("actual-paid per-lot value changed %+v %v", quote, err)
	}
	if !reflect.DeepEqual(beforeMoney, planSyncFinancialSnapshot(t, f.h.db)) {
		t.Fatal("quota sync invented money or rewrote snapshots")
	}
	// Subsequent catalog changes do not resize either add-on or silently select
	// its latest ResetQuotaBytes as the base. Consume original base first.
	completePerpetualFIFOUsage(t, f, after.ID, "base-after-sync-20", 20)
	quota = 300
	if _, err := f.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &updated.Revision}); err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&after, before.ID).Error; err != nil || after.FlowTotal != 450 || after.FlowUsed != 80 || after.ResetQuotaBytes != 300 {
		t.Fatal("second base resize lost original lot attribution", err)
	}
	quote, err = f.h.services.SubscriptionValues.Read(t.Context(), 1, after.ID)
	// The original price remains 10000. 220/300 rounds once using existing math.
	if err != nil || quote.TotalMinor != 14833 {
		t.Fatalf("repeated base resize repriced original or add-on money %+v %v", quote, err)
	}
	if !reflect.DeepEqual(beforeMoney, planSyncFinancialSnapshot(t, f.h.db)) {
		t.Fatal("second resize changed historical financial facts")
	}
}

func TestPlanBaseQuotaSyncPreservesPastAddonConsumption(t *testing.T) {
	assertPlanBaseQuotaPastConsumption(t, newOrderFixture(t))
}
func TestMySQLPlanBaseQuotaSyncPreservesPastAddonConsumption(t *testing.T) {
	assertPlanBaseQuotaPastConsumption(t, newMySQLBusinessOrderFixture(t))
}
func assertPlanBaseQuotaPastConsumption(t *testing.T, base orderFixture) {
	f, root := perpetualValueFixtureWithBase(t, base, 60)
	planBaseSyncAddon(t, f, root.SubscriptionID, 100, 6000)
	completePerpetualFIFOUsage(t, f, root.SubscriptionID, "base-and-addon-past-consumption", 90)
	var before model.Subscription
	var plan model.Plan
	if err := f.h.db.First(&before, root.SubscriptionID).Error; err != nil || before.FlowUsed != 150 {
		t.Fatal(err)
	}
	if err := f.h.db.First(&plan, before.PlanID).Error; err != nil {
		t.Fatal(err)
	}
	money := planSyncFinancialSnapshot(t, f.h.db)
	quota := int64(50)
	updated, err := f.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	var after model.Subscription
	if err := f.h.db.First(&after, before.ID).Error; err != nil || after.FlowUsed != 150 || after.FlowTotal != 200 || after.FlowTotal-after.FlowUsed != 50 {
		t.Fatal("base decrease swallowed already purchased add-on remainder", err)
	}
	quote, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, after.ID)
	if err != nil || quote.TotalMinor != 3000 {
		t.Fatalf("consumed base revived or past addon use reassigned %+v %v", quote, err)
	}
	// Expanding the original base cannot transfer the 50 consumed add-on bytes
	// back into it. The old add-on still has exactly 50 of 100 remaining.
	quota = 200
	if _, err := f.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &updated.Revision}); err != nil {
		t.Fatal(err)
	}
	quote, err = f.h.services.SubscriptionValues.Read(t.Context(), 1, after.ID)
	if err != nil || quote.TotalMinor != 8000 {
		t.Fatalf("FIFO history moved consumed bytes across source lots %+v %v", quote, err)
	}
	if !reflect.DeepEqual(money, planSyncFinancialSnapshot(t, f.h.db)) {
		t.Fatal("base resize funded a wallet or rewrote old prices")
	}
}

func TestPlanBaseQuotaSyncFailureRollsBackProofAndPublication(t *testing.T) {
	assertPlanBaseQuotaRollback(t, newOrderFixture(t), false)
}
func TestMySQLPlanBaseQuotaSyncFailureRollsBackProofAndPublication(t *testing.T) {
	assertPlanBaseQuotaRollback(t, newMySQLBusinessOrderFixture(t), false)
}
func TestPlanBaseQuotaSyncUnknownHistoryDoesNotInventRoot(t *testing.T) {
	assertPlanBaseQuotaRollback(t, newOrderFixture(t), true)
}
func TestMySQLPlanBaseQuotaSyncUnknownHistoryDoesNotInventRoot(t *testing.T) {
	assertPlanBaseQuotaRollback(t, newMySQLBusinessOrderFixture(t), true)
}
func assertPlanBaseQuotaRollback(t *testing.T, base orderFixture, unknown bool) {
	f, root := perpetualValueFixtureWithBase(t, base, 60)
	planBaseSyncAddon(t, f, root.SubscriptionID, 100, 6000)
	if unknown {
		if err := f.h.db.Model(&model.Order{}).Where("id = ?", root.ID).Update("entitlement_value_snapshot", "").Error; err != nil {
			t.Fatal(err)
		}
	}
	var before model.Subscription
	var plan model.Plan
	var intentsBefore []model.NodeConfigPublish
	if err := f.h.db.First(&before, root.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&plan, before.PlanID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Order("node_id").Find(&intentsBefore).Error; err != nil {
		t.Fatal(err)
	}
	money := planSyncFinancialSnapshot(t, f.h.db)
	const hook = "test:base-quota-final-audit-sql-failure"
	var injected atomic.Bool
	if !unknown {
		if err := f.h.db.Callback().Create().Before("gorm:create").Register(hook, func(q *gorm.DB) {
			if audit, ok := q.Statement.Dest.(*model.AuditLog); ok && audit.Action == "plan.update" {
				injected.Store(true)
				q.AddError(q.Exec("INSERT INTO zboard_test_missing_base_sync_audit(id) VALUES(1)").Error)
			}
		}); err != nil {
			t.Fatal(err)
		}
		defer f.h.db.Callback().Create().Remove(hook)
	}
	quota := int64(200)
	request := commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &plan.Revision}
	_, err := f.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, request)
	var invalid *commerce.ValidationError
	if unknown {
		if !errors.As(err, &invalid) || invalid.Fields["sync_traffic_quota"] == "" {
			t.Fatal("missing root provenance became free quota", err)
		}
	} else if err == nil || !injected.Load() {
		t.Fatal("final audit failure committed base sync")
	}
	var after model.Subscription
	var afterPlan model.Plan
	var intentsAfter []model.NodeConfigPublish
	var events int64
	if err := f.h.db.First(&after, before.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&afterPlan, plan.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Order("node_id").Find(&intentsAfter).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.QuotaEvent{}).Where("subscription_id = ? AND event_type = ?", before.ID, "plan_quota_sync").Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(plan, afterPlan) || !reflect.DeepEqual(intentsBefore, intentsAfter) || !reflect.DeepEqual(money, planSyncFinancialSnapshot(t, f.h.db)) || events != 0 {
		t.Fatal("failed mixed sync changed rights/evidence/publication/money")
	}
	if !unknown {
		f.h.db.Callback().Create().Remove(hook)
		if _, err := f.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, request); err != nil {
			t.Fatal("clean retry failed", err)
		}
	}
}

// Financial writers own order before plan/subscription. A plan edit already
// holding plan/sub must fail NOWAIT and roll back, never wait in reverse order.
func TestMySQLPlanBaseQuotaSyncDoesNotWaitOnFinancialOrderLock(t *testing.T) {
	f, root := perpetualValueFixtureWithBase(t, newMySQLBusinessOrderFixture(t), 60)
	planBaseSyncAddon(t, f, root.SubscriptionID, 100, 6000)
	var before model.Subscription
	var plan model.Plan
	if err := f.h.db.First(&before, root.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&plan, before.PlanID).Error; err != nil {
		t.Fatal(err)
	}
	money := planSyncFinancialSnapshot(t, f.h.db)
	blocker := f.h.db.Begin()
	if blocker.Error != nil {
		t.Fatal(blocker.Error)
	}
	defer blocker.Rollback()
	var order model.Order
	if err := blocker.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, root.ID).Error; err != nil {
		t.Fatal(err)
	}
	quota := int64(200)
	request := commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &plan.Revision}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err := f.h.services.PlanUpdate.Update(ctx, 1, plan.ID, request)
	var invalid *commerce.ValidationError
	if errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &invalid) || invalid.Fields["sync_traffic_quota"] == "" {
		t.Fatal("reverse order wait was not refused immediately", err)
	}
	if err := blocker.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	var after model.Subscription
	var afterPlan model.Plan
	if err := f.h.db.First(&after, before.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&afterPlan, plan.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(plan, afterPlan) || !reflect.DeepEqual(money, planSyncFinancialSnapshot(t, f.h.db)) {
		t.Fatal("source lock conflict partially committed sync")
	}
	if _, err := f.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, request); err != nil {
		t.Fatal("clean retry after financial writer release failed", err)
	}
}

func TestPlanBaseQuotaSyncConcurrentActualMetering(t *testing.T) {
	assertPlanBaseQuotaMetering(t, newOrderFixture(t))
}
func TestMySQLPlanBaseQuotaSyncConcurrentActualMetering(t *testing.T) {
	assertPlanBaseQuotaMetering(t, newMySQLBusinessOrderFixture(t))
}
func assertPlanBaseQuotaMetering(t *testing.T, base orderFixture) {
	f, root := perpetualValueFixtureWithBase(t, base, 60)
	planBaseSyncAddon(t, f, root.SubscriptionID, 100, 6000)
	var sub model.Subscription
	var plan model.Plan
	var credential model.ProtocolCredential
	if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&plan, sub.PlanID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Where("subscription_id = ? AND current_scope = ?", sub.ID, 1).First(&credential).Error; err != nil {
		t.Fatal(err)
	}
	beforeMoney := planSyncFinancialSnapshot(t, f.h.db)
	quota := int64(200)
	synchronize := func(ctx context.Context) error {
		_, err := f.h.services.PlanUpdate.Update(ctx, 1, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &plan.Revision})
		return err
	}
	accounting := metering.CompletionAccounting{Repository: meteringstore.CompletionAccounting{DB: f.h.db, Cipher: f.h.credentialCipher, EnqueuePublications: networkstore.EnqueueSubscriptionPublications}}
	event := metering.CompletedFlow{NodeID: credential.NodeID, CoreInstanceID: "base-sync-race", EventID: "base-sync-race-25", EventType: "flow.completed", Sequence: 1, FlowID: "base-sync-race-flow", PrincipalKey: credential.PrincipalKey, BytesDown: 25, OccurredAt: time.Now().UTC()}
	meter := func(ctx context.Context) error { _, _, err := accounting.Complete(ctx, event); return err }
	if syncErr, meterErr := planQuotaInterleave(t, f.h.db, synchronize, meter); syncErr != nil || meterErr != nil {
		t.Fatalf("base sync/metering failed: sync=%v meter=%v", syncErr, meterErr)
	}
	if _, _, err := accounting.Complete(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil || sub.FlowTotal != 300 || sub.FlowUsed != 85 {
		t.Fatal("concurrent original usage lost or assigned to wrong lot", err)
	}
	quote, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, sub.ID)
	if err != nil || quote.TotalMinor != 11750 {
		t.Fatalf("concurrent FIFO valuation wrong %+v %v", quote, err)
	}
	if !reflect.DeepEqual(beforeMoney, planSyncFinancialSnapshot(t, f.h.db)) {
		t.Fatal("concurrent accounting invented finance")
	}
}

func TestPlanBaseQuotaSyncRefusesWrongPersistedEpoch(t *testing.T) {
	assertPlanBaseQuotaWrongEpoch(t, newOrderFixture(t))
}
func TestMySQLPlanBaseQuotaSyncRefusesWrongPersistedEpoch(t *testing.T) {
	assertPlanBaseQuotaWrongEpoch(t, newMySQLBusinessOrderFixture(t))
}
func assertPlanBaseQuotaWrongEpoch(t *testing.T, base orderFixture) {
	f, root := perpetualValueFixtureWithBase(t, base, 60)
	planBaseSyncAddon(t, f, root.SubscriptionID, 100, 6000)
	var plan model.Plan
	if err := f.h.db.First(&plan, f.planRecord.ID).Error; err != nil {
		t.Fatal(err)
	}
	quota := int64(200)
	if _, err := f.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &plan.Revision}); err != nil {
		t.Fatal(err)
	}
	var event model.QuotaEvent
	if err := f.h.db.Where("subscription_id = ? AND event_type = ?", root.SubscriptionID, "plan_quota_sync").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&event).Update("entitlement_epoch", event.EntitlementEpoch+1).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, root.SubscriptionID); !errors.Is(err, commerce.ErrSubscriptionValueUnavailable) {
		t.Fatal("wrong epoch proof became spendable value", err)
	}
}

func TestPlanBaseQuotaSyncWholeUnsubscribeUsesOriginalPaidCaps(t *testing.T) {
	assertPlanBaseQuotaUnsubscribe(t, newOrderFixture(t))
}
func TestMySQLPlanBaseQuotaSyncWholeUnsubscribeUsesOriginalPaidCaps(t *testing.T) {
	assertPlanBaseQuotaUnsubscribe(t, newMySQLBusinessOrderFixture(t))
}
func assertPlanBaseQuotaUnsubscribe(t *testing.T, base orderFixture) {
	f, root := perpetualValueFixtureWithBase(t, base, 60)
	addon := planBaseSyncAddon(t, f, root.SubscriptionID, 100, 6000)
	var plan model.Plan
	if err := f.h.db.First(&plan, f.planRecord.ID).Error; err != nil {
		t.Fatal(err)
	}
	quota := int64(200)
	if _, err := f.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &plan.Revision}); err != nil {
		t.Fatal(err)
	}
	var synchronized model.Subscription
	if err := f.h.db.First(&synchronized, root.SubscriptionID).Error; err != nil || synchronized.FlowTotal != 300 || synchronized.FlowUsed != 60 || synchronized.FlowTotal-synchronized.FlowUsed != 240 {
		t.Fatal("100 base + 100 addon must become 300 total with 60 used", err)
	}
	quote, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, root.SubscriptionID)
	if err != nil || quote.TotalMinor != 13000 {
		t.Fatal("base sync quote exceeds or loses original paid lot value", quote, err)
	}
	request := commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "base-only-sync-whole-unsubscribe"}
	var receiptID uint
	for range 2 {
		receipt, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, root.SubscriptionID, request)
		if err != nil || receipt.AmountMinor != quote.TotalMinor || receiptID != 0 && receiptID != receipt.ID {
			t.Fatal("base sync settlement/replay diverged from quote", receipt, err)
		}
		receiptID = receipt.ID
	}
	var wallet model.WalletAccount
	if err := f.h.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 27000 || wallet.ReservedMinor != 0 {
		t.Fatal("base resize fabricated payment or repeated balance credit", err)
	}
	var afterRoot, afterAddon model.Order
	if err := f.h.db.First(&afterRoot, root.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&afterAddon, addon.ID).Error; err != nil {
		t.Fatal(err)
	}
	if afterRoot.PaidAmount != 10000 || afterRoot.RefundAmount != 7000 || afterRoot.EntitlementValueSnapshot != root.EntitlementValueSnapshot || afterAddon.PaidAmount != 6000 || afterAddon.RefundAmount != 6000 || afterAddon.EntitlementValueSnapshot != addon.EntitlementValueSnapshot {
		t.Fatal("settlement rewrote paid prices/snapshots or crossed source caps")
	}
}

func TestPlanBaseQuotaSyncLaterRenewalDoesNotBecomeBase(t *testing.T) {
	assertPlanBaseQuotaLaterRenewal(t, newOrderFixture(t))
}
func TestMySQLPlanBaseQuotaSyncLaterRenewalDoesNotBecomeBase(t *testing.T) {
	assertPlanBaseQuotaLaterRenewal(t, newMySQLBusinessOrderFixture(t))
}
func assertPlanBaseQuotaLaterRenewal(t *testing.T, base orderFixture) {
	// Renewal verifies the original purchase agreement before accepting a new
	// order. Use an actual accepted initial-only quote, not the separate admin
	// assignment fixture whose manual price has no provider agreement.
	f, root := planBaseQuotaInitialDiscountFixture(t, base, 60)
	planBaseSyncAddon(t, f, root.SubscriptionID, 100, 6000)
	var plan model.Plan
	if err := f.h.db.First(&plan, f.planRecord.ID).Error; err != nil {
		t.Fatal(err)
	}
	quota := int64(200)
	updated, err := f.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	var baseAndAddon model.Subscription
	if err := f.h.db.First(&baseAndAddon, root.SubscriptionID).Error; err != nil || baseAndAddon.FlowTotal != 300 || baseAndAddon.FlowUsed != 60 || baseAndAddon.ResetQuotaBytes != 200 {
		t.Fatal("base100 + addon100 did not become base200 + unchanged addon100", err)
	}
	// Change only today's catalog. A real once renewal buys a separate 50-byte
	// lot and may store 50 as RQB; it cannot redefine the original 200-byte base.
	quota = 50
	updated, err = f.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, ExpectedRevision: &updated.Revision})
	if err != nil {
		t.Fatal(err)
	}
	sku := f.sku(t, plan.ID, 12000, "renew")
	if err := f.h.db.Model(&sku).Updates(map[string]any{"billing_unit": "once", "billing_value": 1, "billing_mode": "one_time", "renewal_effect": "add_quota_only"}).Error; err != nil {
		t.Fatal(err)
	}
	paid := int64(3000)
	assigned, err := f.h.services.OrderAssignment.Assign(t.Context(), 1, commerce.OrderAssignmentRequest{UserID: 1, PlanSKUID: sku.ID, TargetSubscriptionID: root.SubscriptionID, PayableAmount: &paid, RequestID: uuid.NewString(), Note: "actual separate once renewal after base synchronization"})
	if err != nil {
		t.Fatal("create separate renewal from verified initial-only purchase", err)
	}
	if assigned.OrderType != "renewal" || assigned.AmountCents != 12000 || assigned.PayableAmount != 3000 {
		t.Fatalf("renewal changed the locked base or explicit payable: %+v", assigned)
	}
	payments := fundingService(f)
	if _, err := payments.Configure(t.Context(), 1, assigned.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal("allocate separate renewal wallet funding", err)
	}
	if _, err := payments.Pay(t.Context(), 1, assigned.ID); err != nil {
		t.Fatal("fulfill separate renewal from wallet", err)
	}
	var before model.Subscription
	if err := f.h.db.First(&before, root.SubscriptionID).Error; err != nil || before.FlowTotal != 350 || before.ResetQuotaBytes != 50 || before.FlowUsed != 60 {
		t.Fatal("real renewal did not establish its distinct batch", err)
	}
	money := planSyncFinancialSnapshot(t, f.h.db)
	quota = 300
	if _, err := f.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &updated.Revision}); err != nil {
		t.Fatal(err)
	}
	var after model.Subscription
	if err := f.h.db.First(&after, before.ID).Error; err != nil || after.FlowTotal != 450 || after.FlowUsed != 60 || after.ResetQuotaBytes != 300 {
		t.Fatal("latest renewal RQB was resized as the base", err)
	}
	quote, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, after.ID)
	if err != nil || quote.TotalMinor != 17000 {
		t.Fatal("later renewal lost original cash attribution", quote, err)
	}
	if !reflect.DeepEqual(money, planSyncFinancialSnapshot(t, f.h.db)) {
		t.Fatal("sync repriced the latest renewal or original root")
	}
}
