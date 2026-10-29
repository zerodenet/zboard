package handler

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func timedPlanSyncFixture(t *testing.T, base orderFixture) (fullReplacementFixture, model.Order) {
	t.Helper()
	f := newFullReplacementFixture(t, base, false, 60, 0)
	*f.at = f.at.AddDate(0, 0, 5).Add(500 * time.Microsecond)
	sku := base.sku(t, base.planRecord.ID, 2000, "timed-addon")
	if err := f.db.Model(&sku).Updates(map[string]any{"entitlement_mode": "traffic_addon", "traffic_bytes": 100, "billing_unit": "once", "billing_value": 1, "billing_mode": "one_time", "renewal_effect": "none"}).Error; err != nil {
		t.Fatal(err)
	}
	made, err := f.orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: sku.ID, TargetSubscriptionID: f.subscription, IdempotencyKey: "timed-sync-real-addon"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, made.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Pay(t.Context(), 1, made.ID); err != nil {
		t.Fatal(err)
	}
	var paid model.Order
	if err := f.db.First(&paid, made.ID).Error; err != nil {
		t.Fatal(err)
	}
	var proof commerce.EntitlementValueSnapshot
	if json.Unmarshal([]byte(paid.EntitlementValueSnapshot), &proof) != nil || proof.Mode != "timed_quota_addition" || !proof.PaymentVerified || proof.ActualPaidMinor != 2000 {
		t.Fatal("fixture did not retain actual timed add-on payment", proof)
	}
	return f, paid
}
func timedPlanSyncServices(f fullReplacementFixture) (commerce.PlanUpdate, commerce.SubscriptionValues) {
	return commerce.PlanUpdate{Repository: commercestore.PlanUpdate{DB: f.db}}, commerce.SubscriptionValues{Repository: commercestore.SubscriptionValues{DB: f.db, Now: func() time.Time { return *f.at }}}
}
func TestTimedFreePlanSyncPreservesCashAndWholeCancellation(t *testing.T) {
	assertTimedFreePlanSyncWholeCancellation(t, newOrderFixture(t))
}
func TestMySQLTimedFreePlanSyncPreservesCashAndWholeCancellation(t *testing.T) {
	assertTimedFreePlanSyncWholeCancellation(t, newMySQLBusinessOrderFixture(t))
}
func assertTimedFreePlanSyncWholeCancellation(t *testing.T, base orderFixture) {
	f, addon := timedPlanSyncFixture(t, base)
	updates, values := timedPlanSyncServices(f)
	beforeMoney := planSyncFinancialSnapshot(t, f.db)
	quote, err := values.Read(t.Context(), 1, f.subscription)
	if err != nil || quote.CurrentMinor != 2400 || quote.FutureMinor != 11000 || quote.TotalMinor != 13400 {
		t.Fatal("actual paid basis differs before free sync", quote, err)
	}
	var before model.Subscription
	var plan model.Plan
	if err := f.db.First(&before, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&plan, before.PlanID).Error; err != nil {
		t.Fatal(err)
	}
	quota := int64(200)
	request := commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &plan.Revision}
	updated, err := updates.Update(t.Context(), 1, plan.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	var after model.Subscription
	if err := f.db.First(&after, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	want := before
	want.FlowTotal = 300
	want.ResetQuotaBytes = 200
	want.UpdatedAt = after.UpdatedAt
	if !reflect.DeepEqual(after, want) {
		t.Fatalf("free base quota changed add-on, used, term or original reset schedule\ngot=%+v\nwant=%+v", after, want)
	}
	var events []model.QuotaEvent
	if err := f.db.Where("subscription_id=? AND event_type=?", f.subscription, "plan_quota_sync").Find(&events).Error; err != nil || len(events) != 1 {
		t.Fatal("missing/duplicate non-payment sync proof", events, err)
	}
	detail, err := commerce.DecodeTimedPlanQuotaSyncDetail([]byte(events[0].Detail))
	if err != nil || detail.CurrentMinorBefore != 2400 || detail.CurrentMinorAfter != 2400 || detail.OldBaseQuota != 100 || detail.NewBaseQuota != 200 || detail.AddonQuota != 100 || detail.PlanRevision != updated.Revision {
		t.Fatal("free quota restored consumed cash or repriced paid add-on", detail, err)
	}
	quoteAfter, err := values.Read(t.Context(), 1, f.subscription)
	if err != nil || quoteAfter.CurrentMinor != 2400 || quoteAfter.FutureMinor != 11000 || quoteAfter.TotalMinor != 13400 || quoteAfter.Fingerprint == quote.Fingerprint {
		t.Fatal("same amount did not bind the new rights evidence", quoteAfter, err)
	}
	if !reflect.DeepEqual(beforeMoney, planSyncFinancialSnapshot(t, f.db)) {
		t.Fatal("free quota changed payment/order/wallet history")
	}
	if _, err := updates.Update(t.Context(), 1, plan.ID, request); err == nil {
		t.Fatal("stale revision duplicated synchronization")
	}
	var count int64
	if err := f.db.Model(&model.QuotaEvent{}).Where("subscription_id=? AND event_type=?", f.subscription, "plan_quota_sync").Count(&count).Error; err != nil || count != 1 {
		t.Fatal("stale retry created another sync", count, err)
	}
	var walletBefore model.WalletAccount
	if err := f.db.Where("user_id=? AND currency=?", 1, "CNY").First(&walletBefore).Error; err != nil {
		t.Fatal(err)
	}
	terminate := commerce.SubscriptionTermination{Repository: commercestore.SubscriptionTermination{DB: f.db, Now: func() time.Time { return *f.at }}}
	end := commerce.SubscriptionTerminationRequest{QuoteFingerprint: quoteAfter.Fingerprint, IdempotencyKey: "timed-free-sync-whole-cancel"}
	for range 2 {
		receipt, err := terminate.Terminate(t.Context(), 1, f.subscription, end)
		if err != nil || receipt.AmountMinor != 13400 {
			t.Fatal("whole cancellation did not refund only settled remainder exactly once", receipt, err)
		}
	}
	var walletAfter model.WalletAccount
	if err := f.db.Where("user_id=? AND currency=?", 1, "CNY").First(&walletAfter).Error; err != nil || walletAfter.BalanceMinor != walletBefore.BalanceMinor+13400 || walletAfter.ReservedMinor != 0 {
		t.Fatal("free quota minted balance or duplicated refund", walletAfter, err)
	}
	if err := f.db.First(&after, f.subscription).Error; err != nil || after.Status != "canceled" || after.EndedAt == nil {
		t.Fatal("whole subscription rights were not terminated", after, err)
	}
	var roots []model.Order
	if err := f.db.Where("id IN ?", []uint{f.root.ID, addon.ID}).Order("id ASC").Find(&roots).Error; err != nil || len(roots) != 2 {
		t.Fatal(err)
	}
	var refund int64
	for _, row := range roots {
		if row.RefundAmount < 0 || row.RefundAmount > row.PaidAmount || row.SubscriptionEndedAt == nil {
			t.Fatal("refund exceeded original actual source", row)
		}
		refund += row.RefundAmount
	}
	if refund != 13400 {
		t.Fatal("settlement source sum changed", refund)
	}
}
func TestTimedFreePlanSyncFailureRollsBackRightsProofAndPublication(t *testing.T) {
	assertTimedFreePlanSyncRollback(t, newOrderFixture(t))
}
func TestMySQLTimedFreePlanSyncFailureRollsBackRightsProofAndPublication(t *testing.T) {
	assertTimedFreePlanSyncRollback(t, newMySQLBusinessOrderFixture(t))
}
func assertTimedFreePlanSyncRollback(t *testing.T, base orderFixture) {
	f, _ := timedPlanSyncFixture(t, base)
	updates, values := timedPlanSyncServices(f)
	beforeMoney := planSyncFinancialSnapshot(t, f.db)
	var before model.Subscription
	var plan model.Plan
	var beforeIntents []model.NodeConfigPublish
	if err := f.db.First(&before, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&plan, before.PlanID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Order("node_id").Find(&beforeIntents).Error; err != nil {
		t.Fatal(err)
	}
	var injected atomic.Bool
	const hook = "test:timed-plan-free-sync-proof-failure"
	if err := f.db.Callback().Create().Before("gorm:create").Register(hook, func(tx *gorm.DB) {
		row, ok := tx.Statement.Dest.(*model.QuotaEvent)
		if ok && row.EventType == "plan_quota_sync" {
			injected.Store(true)
			tx.AddError(tx.Exec("INSERT INTO zboard_test_missing_timed_sync_proof(id) VALUES(1)").Error)
		}
	}); err != nil {
		t.Fatal(err)
	}
	quota := int64(200)
	request := commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &plan.Revision}
	_, err := updates.Update(t.Context(), 1, plan.ID, request)
	f.db.Callback().Create().Remove(hook)
	if err == nil || !injected.Load() || !strings.Contains(err.Error(), "zboard_test_missing_timed_sync_proof") {
		t.Fatal("real statement fault not exercised", err)
	}
	var after model.Subscription
	var planAfter model.Plan
	var afterIntents []model.NodeConfigPublish
	if err := f.db.First(&after, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&planAfter, plan.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Order("node_id").Find(&afterIntents).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(plan, planAfter) || !reflect.DeepEqual(beforeIntents, afterIntents) || !reflect.DeepEqual(beforeMoney, planSyncFinancialSnapshot(t, f.db)) {
		t.Fatal("failed proof partially committed rights, plan, publication or money")
	}
	var count int64
	if err := f.db.Model(&model.QuotaEvent{}).Where("subscription_id=? AND event_type=?", f.subscription, "plan_quota_sync").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("failed proof survived rollback", count, err)
	}
	if _, err := updates.Update(t.Context(), 1, plan.ID, request); err != nil {
		t.Fatal("retry after full rollback failed", err)
	}
	quote, err := values.Read(t.Context(), 1, f.subscription)
	if err != nil || quote.TotalMinor != 13400 {
		t.Fatal(quote, err)
	}
}
func TestTimedFreePlanSyncMissingCapNeverRestoresOldValue(t *testing.T) {
	assertTimedFreePlanSyncMissingCap(t, newOrderFixture(t))
}
func TestMySQLTimedFreePlanSyncMissingCapNeverRestoresOldValue(t *testing.T) {
	assertTimedFreePlanSyncMissingCap(t, newMySQLBusinessOrderFixture(t))
}
func assertTimedFreePlanSyncMissingCap(t *testing.T, base orderFixture) {
	f, _ := timedPlanSyncFixture(t, base)
	updates, values := timedPlanSyncServices(f)
	var sub model.Subscription
	var plan model.Plan
	if err := f.db.First(&sub, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&plan, sub.PlanID).Error; err != nil {
		t.Fatal(err)
	}
	quota := int64(200)
	if _, err := updates.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &plan.Revision}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Where("subscription_id=? AND event_type=?", sub.ID, "plan_quota_sync").Delete(&model.QuotaEvent{}).Error; err != nil {
		t.Fatal(err)
	}
	if quote, err := values.Read(t.Context(), 1, sub.ID); !errors.Is(err, commerce.ErrSubscriptionValueUnavailable) {
		t.Fatal("missing settled cap silently revived original paid basis", quote, err)
	}
}
