package handler

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestAdminSubscriptionResetPolicyCorrectionPreservesUsageAndPaidFacts(t *testing.T) {
	f := newOrderFixture(t)
	paid := f.paid(t, f.create(t, 0).ID)
	now := time.Now().UTC()
	stale := now.AddDate(0, -1, 0)
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Updates(map[string]any{"start_at": now.AddDate(0, -2, 0), "end_at": now.AddDate(1, 0, 0), "flow_used": 600, "reset_policy": 1, "next_reset_at": stale}).Error; err != nil {
		t.Fatal(err)
	}
	var original model.Order
	f.h.db.First(&original, paid.ID)
	policy := int16(2)
	in := entitlements.SubscriptionQuotaInput{ResetPolicy: &policy, Reason: "修正历史订阅重置策略", IdempotencyKey: "reset-policy-only"}
	for range 2 {
		if w := quotaRequest(t, f, paid.SubscriptionID, in); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	var sub model.Subscription
	f.h.db.First(&sub, paid.SubscriptionID)
	expected := entitlements.NextTrafficResetAfter(sub.StartAt, policy, now)
	if sub.ResetPolicy != 2 || sub.NextResetAt == nil || !sub.NextResetAt.Equal(*expected) || sub.FlowUsed != 600 || sub.FlowTotal != 1024 || sub.ResetQuotaBytes != 1024 || sub.CycleStartUsed != 0 {
		t.Fatal("prospective correction changed current rights", sub)
	}
	var events []model.QuotaEvent
	f.h.db.Where("subscription_id = ? AND event_type IN ?", sub.ID, []string{"admin_quota", "reset"}).Find(&events)
	if len(events) != 1 || events[0].EventType != "admin_quota" || events[0].DeltaBytes != 0 {
		t.Fatal("correction granted or replayed a reset", events)
	}
	var detail struct{ Before, After map[string]json.RawMessage }
	if err := json.Unmarshal([]byte(events[0].Detail), &detail); err != nil || string(detail.Before["reset_policy"]) != "1" || string(detail.After["reset_policy"]) != "2" {
		t.Fatal("missing schedule audit", events[0].Detail, err)
	}
	var afterOrder model.Order
	f.h.db.First(&afterOrder, paid.ID)
	if !reflect.DeepEqual(original, afterOrder) {
		t.Fatal("paid source rewritten")
	}
	policy = 5
	if w := quotaRequest(t, f, paid.SubscriptionID, in); w.Code != 409 {
		t.Fatal("changed idempotent retry accepted", w.Code)
	}
	in.IdempotencyKey = "disable-reset"
	if w := quotaRequest(t, f, paid.SubscriptionID, in); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var disabled model.Subscription
	f.h.db.First(&disabled, sub.ID)
	if disabled.NextResetAt != nil || disabled.ResetPolicy != 5 || disabled.FlowUsed != 600 {
		t.Fatal("disable did not clear schedule", disabled)
	}
}

func TestAdminSubscriptionResetPolicyRejectsFixedQuotaAndInvalidPolicy(t *testing.T) {
	f := newOrderFixture(t)
	paid := f.paid(t, f.create(t, 0).ID)
	f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Updates(map[string]any{"ends_on_quota_exhaustion": true, "lifecycle": "fixed"})
	for _, policy := range []int16{-1, 2, 6} {
		if w := quotaRequest(t, f, paid.SubscriptionID, entitlements.SubscriptionQuotaInput{ResetPolicy: &policy, Reason: "修改重置策略", IdempotencyKey: "invalid-policy"}); w.Code != 400 {
			t.Fatal("invalid periodic reset accepted", policy, w.Code)
		}
	}
}

func TestPlanResetPolicySyncPagesIndependentlyOfLegacyPaymentEvidence(t *testing.T) {
	f := newOrderFixture(t)
	paid := f.paid(t, f.create(t, 0).ID)
	var base model.Subscription
	f.h.db.First(&base, paid.SubscriptionID)
	// The exact production failure: ordinary cycle, unchanged base quota and
	// legacy payment evidence that cannot authorize a real monetary resize.
	if err := f.h.db.Model(&model.Order{}).Where("id = ?", paid.ID).Update("entitlement_value_snapshot", "invalid legacy source").Error; err != nil {
		t.Fatal(err)
	}
	var oldOrder model.Order
	f.h.db.First(&oldOrder, paid.ID)
	if oldOrder.EntitlementValueSnapshot != "invalid legacy source" {
		t.Fatal("legacy source fixture was not installed")
	}
	rows := make([]model.Subscription, 130)
	for i := range rows {
		rows[i] = base
		rows[i].ID = 0
		rows[i].NextResetAt = nil
		rows[i].ResetPolicy = 0
	}
	if err := f.h.db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	excluded := []model.Subscription{base, base, base, base}
	for i := range excluded {
		excluded[i].ID = 0
		excluded[i].ResetPolicy = 0
		excluded[i].NextResetAt = nil
	}
	excluded[0].EndedAt = &now
	excluded[1].EndsOnQuotaExhaustion = true
	excluded[1].Lifecycle = "fixed"
	excluded[2].EndAt = entitlements.PerpetualEnd
	excluded[3].EndAt = now.Add(-time.Hour)
	if err := f.h.db.Create(&excluded).Error; err != nil {
		t.Fatal(err)
	}
	var plan model.Plan
	f.h.db.First(&plan, base.PlanID)
	policy, quota := int16(2), base.ResetQuotaBytes
	if _, err := planQuotaUpdateService(f.h.db).Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{ResetPolicy: &policy, SyncResetPolicy: true, TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &plan.Revision}); err != nil {
		t.Fatal(err)
	}
	var changed []model.Subscription
	f.h.db.Where("plan_id = ?", plan.ID).Find(&changed)
	if len(changed) != 135 {
		t.Fatal(len(changed))
	}
	for _, sub := range changed {
		if sub.ID >= excluded[0].ID {
			if sub.ResetPolicy != 0 || sub.NextResetAt != nil {
				t.Fatal("excluded subscription schedule changed", sub)
			}
			continue
		}
		if sub.ResetPolicy != 2 || sub.NextResetAt == nil || !sub.NextResetAt.After(time.Now().UTC()) || sub.FlowTotal != base.FlowTotal || sub.FlowUsed != base.FlowUsed || sub.ResetQuotaBytes != base.ResetQuotaBytes {
			t.Fatal("schedule-only batch changed quota or missed a page", sub)
		}
	}
	var events int64
	f.h.db.Model(&model.QuotaEvent{}).Where("event_type = ?", "reset_policy_sync").Count(&events)
	if events != 131 {
		t.Fatal("missing bounded audit", events)
	}
	var afterOrder model.Order
	f.h.db.First(&afterOrder, paid.ID)
	if !reflect.DeepEqual(oldOrder, afterOrder) {
		t.Fatal("sync altered paid evidence")
	}
}

func TestPlanResetPolicySyncRollsBackOnEvidenceWriteFailure(t *testing.T) {
	f := newOrderFixture(t)
	paid := f.paid(t, f.create(t, 0).ID)
	var before model.Subscription
	f.h.db.First(&before, paid.SubscriptionID)
	var plan model.Plan
	f.h.db.First(&plan, before.PlanID)
	rows := make([]model.Subscription, 130)
	for i := range rows {
		rows[i] = before
		rows[i].ID = 0
	}
	if err := f.h.db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	var beforeRows []model.Subscription
	f.h.db.Order("id ASC").Find(&beforeRows)
	const hook = "test:reject-reset-policy-event"
	writes := 0
	f.h.db.Callback().Create().Before("gorm:create").Register(hook, func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*model.QuotaEvent); ok && event.EventType == "reset_policy_sync" {
			writes++
			if writes == 129 {
				tx.AddError(errors.New("test second-page evidence write failure"))
			}
		}
	})
	defer f.h.db.Callback().Create().Remove(hook)
	policy := int16(2)
	if _, err := planQuotaUpdateService(f.h.db).Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{ResetPolicy: &policy, SyncResetPolicy: true, ExpectedRevision: &plan.Revision}); err == nil {
		t.Fatal("event failure was ignored")
	}
	var after model.Subscription
	f.h.db.First(&after, before.ID)
	var afterPlan model.Plan
	f.h.db.First(&afterPlan, plan.ID)
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(plan, afterPlan) {
		t.Fatal("failed schedule sync was partially committed")
	}
	var afterRows []model.Subscription
	f.h.db.Order("id ASC").Find(&afterRows)
	if writes != 129 || !reflect.DeepEqual(beforeRows, afterRows) {
		t.Fatal("second-page failure changed earlier subscriptions", writes)
	}
}
