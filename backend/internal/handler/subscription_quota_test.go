package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func quotaRequest(t *testing.T, f orderFixture, id uint, in entitlements.SubscriptionQuotaInput) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	f.h.AdminSubscriptionQuotaHandler(w, announcementRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/subscriptions/%d/quota", id), f.token, string(body)))
	return w
}

func TestAdminSubscriptionQuotaRestoresUsageAndPreservesHistory(t *testing.T) {
	f := newOrderFixture(t)
	endpoint := attachOrderPublishEndpoint(t, f)
	paid := f.paid(t, f.create(t, 0).ID)
	var before model.Subscription
	f.h.db.First(&before, paid.SubscriptionID)
	// Simulate an exhausted cycle following an earlier cycle, including the
	// old release's persisted expired state.
	f.h.db.Model(&before).Updates(map[string]any{"flow_total": 3024, "flow_used": 3024, "cycle_start_used": 2000, "status": "expired"})
	if err := expireSubscriptions(f.h.db, 1, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	record := model.TrafficRecord{SubscriptionID: before.ID, NodeID: endpoint.NodeID, ReportID: "original-usage", Nonce: "original-usage", UsedBytes: 1024}
	if err := f.h.db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	in := entitlements.SubscriptionQuotaInput{FlowTotal: 2048, FlowUsed: 512, ResetQuotaBytes: 4096, ExpectedFlowTotal: 1024, ExpectedFlowUsed: 1024, ExpectedResetQuotaBytes: 1024, Reason: "客服修正计费用量", IdempotencyKey: "adjust-1"}
	w := quotaRequest(t, f, before.ID, in)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var after model.Subscription
	if err := f.h.db.First(&after, before.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.FlowTotal != 4048 || after.FlowUsed != 2512 || after.CycleStartUsed != 2000 || after.ResetQuotaBytes != 4096 || after.Status != "active" || !after.EndAt.Equal(before.EndAt) || after.PlanID != before.PlanID || after.PlanSKUID != before.PlanSKUID {
		t.Fatal(after)
	}
	var credential model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ?", before.ID).First(&credential).Error; err != nil || credential.Status != "active" {
		t.Fatal("credentials not restored", credential, err)
	}
	var original model.TrafficRecord
	if err := f.h.db.First(&original, record.ID).Error; err != nil || original.UsedBytes != 1024 {
		t.Fatal("historical usage changed", original, err)
	}
	if w := quotaRequest(t, f, before.ID, in); w.Code != 200 {
		t.Fatal("replay", w.Code, w.Body.String())
	}
	var count int64
	f.h.db.Model(&model.QuotaEvent{}).Where("event_type = ?", "admin_quota").Count(&count)
	if count != 1 {
		t.Fatal("adjustment replayed", count)
	}
	f.h.db.Model(&model.AuditLog{}).Where("action = ?", "subscription.quota.update").Count(&count)
	if count != 1 {
		t.Fatal("audit missing", count)
	}
	in.FlowUsed++
	if w := quotaRequest(t, f, before.ID, in); w.Code != 409 {
		t.Fatal("changed replay", w.Code)
	}
	in.IdempotencyKey = "stale"
	if w := quotaRequest(t, f, before.ID, in); w.Code != 409 {
		t.Fatal("concurrent change overwritten", w.Code)
	}
	// Exhausting an unexpired service suspends credentials without expiring it.
	in = entitlements.SubscriptionQuotaInput{FlowTotal: 2048, FlowUsed: 2048, ResetQuotaBytes: 4096, ExpectedFlowTotal: 2048, ExpectedFlowUsed: 512, ExpectedResetQuotaBytes: 4096, Reason: "设置当前周期用量", IdempotencyKey: "exhaust"}
	if w := quotaRequest(t, f, before.ID, in); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	f.h.db.First(&after, before.ID)
	f.h.db.First(&credential, credential.ID)
	if after.Status != "active" || credential.Status != "expired" {
		t.Fatal("exhaustion state", after.Status, credential.Status)
	}
	var page struct {
		Items []adminSubscriptionListItem
		Total int64
	}
	if code := f.get(t, "/api/v1/subscriptions?paged=true&status=active", f.h.SubscriptionsHandler, &page); code != 200 || page.Total != 1 || page.Items[0].QuotaStatus != "exhausted" {
		t.Fatal("exhausted instance disappeared", code, page)
	}
	var publish model.NodeConfigPublish
	if err := f.h.db.First(&publish, endpoint.NodeID).Error; err != nil {
		t.Fatal("publication missing", err)
	}
}

func TestAdminSubscriptionQuotaRejectsUnauthorizedAndRollsBackPublicationFailure(t *testing.T) {
	f := newOrderFixture(t)
	attachOrderPublishEndpoint(t, f)
	paid := f.paid(t, f.create(t, 0).ID)
	in := entitlements.SubscriptionQuotaInput{FlowTotal: 2000, FlowUsed: 0, ResetQuotaBytes: 1024, ExpectedFlowTotal: 1024, ExpectedFlowUsed: 0, ExpectedResetQuotaBytes: 1024, Reason: "客服调整配额", IdempotencyKey: "failure"}
	f.h.db.Model(&model.User{}).Where("id = 1").Update("is_admin", false)
	if w := quotaRequest(t, f, paid.SubscriptionID, in); w.Code != 403 {
		t.Fatal("stale admin claim accepted", w.Code)
	}
	f.h.db.Model(&model.User{}).Where("id = 1").Update("is_admin", true)
	in.FlowTotal = -1
	if w := quotaRequest(t, f, paid.SubscriptionID, in); w.Code != 400 {
		t.Fatal("negative quota", w.Code)
	}
	in.FlowTotal = 2000
	clearPublishTestJobs(t, f)
	rejectPublishInsert(t, f)
	if w := quotaRequest(t, f, paid.SubscriptionID, in); w.Code != 500 {
		t.Fatal("publish failure", w.Code, w.Body.String())
	}
	var sub model.Subscription
	f.h.db.First(&sub, paid.SubscriptionID)
	if sub.FlowTotal != 1024 {
		t.Fatal("partial update", sub)
	}
	var count int64
	f.h.db.Model(&model.QuotaEvent{}).Where("event_type = ?", "admin_quota").Count(&count)
	if count != 0 {
		t.Fatal("failed change logged", count)
	}
}

func TestLegacyExhaustedSubscriptionIsVisibleBeforeAnyMutation(t *testing.T) {
	f := newOrderFixture(t)
	paid := f.paid(t, f.create(t, 0).ID)
	f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Updates(map[string]any{"status": "expired", "flow_used": 1024})
	for _, suffix := range []string{"&status=active", "&eligible_for=reset"} {
		var page struct {
			Items []adminSubscriptionListItem
			Total int64
		}
		if code := f.get(t, "/api/v1/subscriptions?paged=true"+suffix, f.h.SubscriptionsHandler, &page); code != 200 || page.Total != 1 || page.Items[0].Status != "active" || page.Items[0].QuotaStatus != "exhausted" {
			t.Fatal(code, page)
		}
	}
	var expiredPage struct {
		Items []adminSubscriptionListItem
		Total int64
	}
	if code := f.get(t, "/api/v1/subscriptions?paged=true&status=expired", f.h.SubscriptionsHandler, &expiredPage); code != 200 || expiredPage.Total != 0 {
		t.Fatal("exhausted instance also matched expired filter", code, expiredPage)
	}
	var sub model.Subscription
	f.h.db.First(&sub, paid.SubscriptionID)
	if sub.Status != "expired" {
		t.Fatal("read mutated stored lifecycle")
	}
	f.h.db.Model(&sub).Update("end_at", time.Now().UTC().Add(-time.Hour))
	var page struct {
		Items []adminSubscriptionListItem
		Total int64
	}
	if code := f.get(t, "/api/v1/subscriptions?paged=true&status=expired", f.h.SubscriptionsHandler, &page); code != 200 || page.Total != 1 || page.Items[0].Status != "expired" {
		t.Fatal("actual expiry changed", code, page)
	}
}
