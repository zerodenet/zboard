package handler

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
	"time"
)

func TestAdminSubscriptionDatesPreserveLatestUsageAndHistoricalOrders(t *testing.T) {
	f := newOrderFixture(t)
	endpoint := attachOrderPublishEndpoint(t, f)
	paid := f.paid(t, f.create(t, 0).ID)
	var sub model.Subscription
	if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	// Legacy financial evidence must not block an administrative date correction.
	if err := f.h.db.Model(&model.Order{}).Where("id=?", paid.ID).Update("purchase_snapshot", "").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&paid, paid.ID).Error; err != nil {
		t.Fatal(err)
	}
	beforeOrder := paid
	old := time.Now().UTC().Add(-time.Hour)
	policy := int16(2)
	if err := f.h.db.Model(&sub).Updates(map[string]any{"flow_used": 600, "reset_policy": policy, "next_reset_at": old}).Error; err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().UTC().AddDate(0, 2, 0).Truncate(time.Millisecond)
	reset := time.Now().UTC().AddDate(0, 0, 3).Truncate(time.Millisecond)
	expectedReset := old.Format(time.RFC3339Nano)
	in := entitlements.SubscriptionQuotaInput{EndAt: &expiry, ExpectedEndAt: &sub.EndAt, NextResetAt: &reset, ExpectedNextResetAt: &expectedReset, Reason: "管理员修正日期", IdempotencyKey: "dates"}
	if w := quotaRequest(t, f, sub.ID, in); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var after model.Subscription
	f.h.db.First(&after, sub.ID)
	if !after.EndAt.Equal(expiry) || after.NextResetAt == nil || !after.NextResetAt.Equal(reset) || after.FlowUsed != 600 || after.FlowTotal != sub.FlowTotal || after.CycleStartUsed != sub.CycleStartUsed || after.ResetQuotaBytes != sub.ResetQuotaBytes {
		t.Fatalf("date edit altered current usage: %+v", after)
	}
	var credential model.ProtocolCredential
	if err := f.h.db.Where("subscription_id=? AND current_scope=1", sub.ID).First(&credential).Error; err != nil || !credential.ExpiresAt.Equal(expiry) {
		t.Fatalf("credential expiry not published: %+v %v", credential, err)
	}
	var publish model.NodeConfigPublish
	if err := f.h.db.First(&publish, endpoint.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	var historical model.Order
	f.h.db.First(&historical, paid.ID)
	if historical.PaidAmount != beforeOrder.PaidAmount || historical.PurchaseSnapshot != beforeOrder.PurchaseSnapshot || historical.EntitlementValueSnapshot != beforeOrder.EntitlementValueSnapshot || historical.Status != beforeOrder.Status {
		t.Fatal("date edit rewrote historical payment")
	}
	f.h.db.Model(&after).Update("flow_used", 700)
	if w := quotaRequest(t, f, sub.ID, in); w.Code != 200 {
		t.Fatal("retry rejected", w.Body.String())
	}
	f.h.db.First(&after, sub.ID)
	if after.FlowUsed != 700 {
		t.Fatal("retry clobbered newer traffic")
	}
	var n int64
	f.h.db.Model(&model.QuotaEvent{}).Where("subscription_id=? AND event_type='admin_quota'", sub.ID).Count(&n)
	if n != 1 {
		t.Fatal("replayed edit event", n)
	}
	var event model.QuotaEvent
	f.h.db.Where("subscription_id=? AND event_type='admin_quota'", sub.ID).First(&event)
	if event.DeltaBytes != 0 || event.BalanceBefore != event.BalanceAfter {
		t.Fatal("date edit granted/reset traffic", event)
	}
}
func TestAdminSubscriptionDateEditDetectsConcurrentRenewalAndReset(t *testing.T) {
	f := newOrderFixture(t)
	paid := f.paid(t, f.create(t, 0).ID)
	var sub model.Subscription
	f.h.db.First(&sub, paid.SubscriptionID)
	expiry := sub.EndAt.AddDate(0, 1, 0)
	stale := sub.EndAt.Add(-time.Hour)
	if w := quotaRequest(t, f, sub.ID, entitlements.SubscriptionQuotaInput{EndAt: &expiry, ExpectedEndAt: &stale, Reason: "调整到期日期", IdempotencyKey: "stale-expiry"}); w.Code != 409 {
		t.Fatal("stale expiry accepted", w.Code, w.Body.String())
	}
	reset := time.Now().UTC().AddDate(0, 0, 1)
	empty := ""
	policy := int16(2)
	stored := reset.Add(time.Hour)
	f.h.db.Model(&sub).Updates(map[string]any{"reset_policy": policy, "next_reset_at": stored})
	if w := quotaRequest(t, f, sub.ID, entitlements.SubscriptionQuotaInput{NextResetAt: &reset, ExpectedNextResetAt: &empty, Reason: "调整重置日期", IdempotencyKey: "stale-reset"}); w.Code != 409 {
		t.Fatal("stale reset accepted", w.Code, w.Body.String())
	}
	f.h.db.First(&sub, sub.ID)
	if sub.NextResetAt == nil || !sub.NextResetAt.Equal(stored) || sub.EndAt.Equal(expiry) {
		t.Fatal("conflicting date edit changed current state")
	}
}
func TestAdminSubscriptionExpiryUpdatesAccessAndRestoresRetainedExpiredService(t *testing.T) {
	f := newOrderFixture(t)
	attachOrderPublishEndpoint(t, f)
	paid := f.paid(t, f.create(t, 0).ID)
	var sub model.Subscription
	f.h.db.First(&sub, paid.SubscriptionID)
	past := time.Now().UTC().Add(-time.Hour)
	start := past.AddDate(0, 0, -10)
	f.h.db.Model(&sub).Update("start_at", start)
	if w := quotaRequest(t, f, sub.ID, entitlements.SubscriptionQuotaInput{EndAt: &past, Reason: "立即到期处理", IdempotencyKey: "expire"}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var cred model.ProtocolCredential
	f.h.db.Where("subscription_id=? AND current_scope=1", sub.ID).First(&cred)
	f.h.db.First(&sub, sub.ID)
	if sub.Status != "expired" || cred.Status != "expired" || !cred.ExpiresAt.Equal(past.Truncate(time.Millisecond)) {
		t.Fatalf("old access survived expiry: %+v %+v", sub, cred)
	}
	f.h.db.Model(&sub).Updates(map[string]any{"ended_at": past, "end_reason": "expired", "reset_policy": 2, "next_reset_at": past, "flow_used": 200})
	future := time.Now().UTC().AddDate(0, 1, 0)
	if w := quotaRequest(t, f, sub.ID, entitlements.SubscriptionQuotaInput{EndAt: &future, Reason: "管理员延长服务", IdempotencyKey: "restore"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	sub = model.Subscription{}
	f.h.db.First(&sub, paid.SubscriptionID)
	cred = model.ProtocolCredential{}
	f.h.db.Where("subscription_id=? AND current_scope=1", sub.ID).First(&cred)
	if sub.EndedAt != nil || sub.EndReason != "" || sub.Status != "active" || cred.Status != "active" || !cred.ExpiresAt.Equal(future.Truncate(time.Millisecond)) || sub.FlowUsed != 200 || sub.NextResetAt == nil || !sub.NextResetAt.After(time.Now()) {
		t.Fatalf("restore failed or cleared usage: %+v %+v", sub, cred)
	}
}
func TestAdminSubscriptionScheduleValidationAndAtomicPublication(t *testing.T) {
	f := newOrderFixture(t)
	attachOrderPublishEndpoint(t, f)
	paid := f.paid(t, f.create(t, 0).ID)
	var sub model.Subscription
	f.h.db.First(&sub, paid.SubscriptionID)
	past := sub.StartAt.Add(-time.Hour)
	after := sub.EndAt.Add(time.Hour)
	for i, in := range []entitlements.SubscriptionQuotaInput{{EndAt: &past}, {NextResetAt: &after}} {
		in.Reason = "日期校验测试"
		in.IdempotencyKey = fmt.Sprintf("invalid-%d", i)
		if w := quotaRequest(t, f, sub.ID, in); w.Code != 400 {
			t.Fatal("invalid date accepted", w.Code, w.Body.String())
		}
	}
	f.h.db.Model(&model.User{}).Where("id=1").Update("is_admin", false)
	in := entitlements.SubscriptionQuotaInput{EndAt: &after, Reason: "管理员延长日期", IdempotencyKey: "atomic-date"}
	if w := quotaRequest(t, f, sub.ID, in); w.Code != 403 {
		t.Fatal("non-admin date edit accepted", w.Code)
	}
	f.h.db.Model(&model.User{}).Where("id=1").Update("is_admin", true)
	clearPublishTestJobs(t, f)
	rejectPublishInsert(t, f)
	if w := quotaRequest(t, f, sub.ID, in); w.Code != 500 {
		t.Fatal("failed publication committed date", w.Code, w.Body.String())
	}
	var current model.Subscription
	f.h.db.First(&current, sub.ID)
	if !current.EndAt.Equal(sub.EndAt) {
		t.Fatal("expiry committed without publication")
	}
	var cred model.ProtocolCredential
	f.h.db.Where("subscription_id=? AND current_scope=1", sub.ID).First(&cred)
	if !cred.ExpiresAt.Equal(sub.EndAt) {
		t.Fatal("credential update escaped rollback")
	}
	var n int64
	f.h.db.Model(&model.QuotaEvent{}).Where("event_type='admin_quota'").Count(&n)
	if n != 0 {
		t.Fatal("failed date edit logged", n)
	}
}
