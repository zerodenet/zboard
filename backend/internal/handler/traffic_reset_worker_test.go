package handler

import (
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestTrafficResetRestoresBaseQuotaWithoutRepeatingAddon(t *testing.T) {
	f := newOrderFixture(t)
	if err := f.h.db.Model(&f.planRecord).Update("reset_policy", 2).Error; err != nil {
		t.Fatal(err)
	}
	paid := f.paid(t, f.create(t, 0).ID)
	now := time.Now().UTC()
	due := now.Add(-time.Hour)
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Updates(map[string]any{
		"flow_total": 1536, "flow_used": 800, "next_reset_at": due,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.runDueTrafficResets(now); err != nil {
		t.Fatal(err)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if sub.ResetQuotaBytes != 1024 || sub.FlowUsed != 800 || sub.FlowTotal != 1824 || sub.CycleStartUsed != 800 {
		t.Fatalf("reset subscription = %+v; want lifetime used 800 and current quota 1024", sub)
	}
	if total, used := subscriptionCycleQuota(sub); total != 1024 || used != 0 {
		t.Fatalf("cycle quota = %d/%d, want 0/1024", used, total)
	}
	var page struct{ Items []adminSubscriptionListItem }
	if status := f.get(t, "/api/v1/subscriptions?paged=true", f.h.SubscriptionsHandler, &page); status != 200 {
		t.Fatalf("subscription page status = %d", status)
	}
	if len(page.Items) != 1 || page.Items[0].FlowTotal != 1024 || page.Items[0].FlowUsed != 0 {
		t.Fatalf("subscription page after reset = %+v", page.Items)
	}
	if sub.NextResetAt == nil || !sub.NextResetAt.After(now) {
		t.Fatalf("next reset = %v, want future date", sub.NextResetAt)
	}
	var event model.QuotaEvent
	if err := f.h.db.Where("subscription_id = ? AND event_type = ?", sub.ID, "reset").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.BalanceBefore != 736 || event.BalanceAfter != 1024 || event.DeltaBytes != 288 {
		t.Fatalf("reset event = %+v", event)
	}
	if err := f.h.runDueTrafficResets(now); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := f.h.db.Model(&model.QuotaEvent{}).Where("subscription_id = ? AND event_type = ?", sub.ID, "reset").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("reset event count = %d, want 1", count)
	}
	if err := f.h.db.Model(&sub).Update("flow_used", 900).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&sub, sub.ID).Error; err != nil {
		t.Fatal(err)
	}
	if total, used := subscriptionCycleQuota(sub); total != 1024 || used != 100 {
		t.Fatalf("cycle quota after usage = %d/%d, want 100/1024", used, total)
	}
}

func TestTrafficResetCatchupPreservesCurrentCycleUsageAndAddon(t *testing.T) {
	f := newOrderFixture(t)
	if err := f.h.db.Model(&f.planRecord).Update("reset_policy", 2).Error; err != nil {
		t.Fatal(err)
	}
	paid := f.paid(t, f.create(t, 0).ID)
	now := time.Now().UTC()
	anchor := now.AddDate(0, -3, 0).Add(-5 * time.Hour)
	due := nextTrafficReset(anchor, 2)
	cycleStart := *due
	for {
		next := nextTrafficResetAfter(anchor, 2, cycleStart)
		if next.After(now) {
			break
		}
		cycleStart = *next
	}
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Updates(map[string]any{
		"start_at": anchor, "end_at": now.AddDate(0, 1, 0), "next_reset_at": due,
		"flow_total": 1536, "flow_used": 1000,
	}).Error; err != nil {
		t.Fatal(err)
	}
	for _, event := range []model.QuotaEvent{
		{SubscriptionID: paid.SubscriptionID, EventType: "traffic_pack", DeltaBytes: 512,
			ReferenceType: "order", ReferenceID: "old-addon", CreatedAt: cycleStart.Add(-time.Hour)},
		{SubscriptionID: paid.SubscriptionID, EventType: "traffic_pack", DeltaBytes: 512,
			ReferenceType: "order", ReferenceID: "current-addon", CreatedAt: cycleStart.Add(time.Minute)},
	} {
		if err := f.h.db.Create(&event).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, record := range []model.TrafficRecord{
		{SubscriptionID: paid.SubscriptionID, NodeID: 1, ReportID: "old-usage", Nonce: "old-usage",
			UsedBytes: 900, At: cycleStart.Add(-time.Hour), CreatedAt: cycleStart.Add(-time.Hour)},
		{SubscriptionID: paid.SubscriptionID, NodeID: 1, ReportID: "current-usage", Nonce: "current-usage",
			UsedBytes: 100, At: cycleStart.Add(time.Minute), CreatedAt: cycleStart.Add(time.Minute)},
	} {
		if err := f.h.db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := f.h.runDueTrafficResets(now); err != nil {
		t.Fatal(err)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if total, used := subscriptionCycleQuota(sub); total != 1536 || used != 100 || sub.FlowTotal-sub.FlowUsed != 1436 {
		t.Fatalf("catch-up cycle = %+v (total=%d used=%d), want 1536/100", sub, total, used)
	}
	if sub.NextResetAt == nil || !sub.NextResetAt.After(now) {
		t.Fatalf("next reset after catch-up = %v", sub.NextResetAt)
	}
}

func TestTrafficResetReactivatesQuotaExhaustedSubscription(t *testing.T) {
	f := newOrderFixture(t)
	endpoint := attachOrderPublishEndpoint(t, f)
	if err := f.h.db.Model(&f.planRecord).Update("reset_policy", 1).Error; err != nil {
		t.Fatal(err)
	}
	paid := f.paid(t, f.create(t, 0).ID)
	clearPublishTestJobs(t, f)
	now := time.Now().UTC()
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Updates(map[string]any{
		"flow_total": 1536, "flow_used": 1536, "status": subStatusExpired,
		"next_reset_at": now.Add(-time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.ProtocolCredential{}).Where("subscription_id = ?", paid.SubscriptionID).
		Update("status", "expired").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.runDueTrafficResets(now); err != nil {
		t.Fatal(err)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if sub.Status != subStatusActive || sub.FlowTotal-sub.FlowUsed != 1024 {
		t.Fatalf("subscription after reset = %+v, want active with 1024 remaining", sub)
	}
	var credential model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ?", sub.ID).First(&credential).Error; err != nil {
		t.Fatal(err)
	}
	if credential.Status != protocolCredentialStatusActive {
		t.Fatalf("credential status = %s, want active", credential.Status)
	}
	var publish model.NodeConfigPublish
	if err := f.h.db.First(&publish, endpoint.NodeID).Error; err != nil {
		t.Fatalf("reset did not enqueue node publication: %v", err)
	}
}

func TestTrafficResetKeepsPurchaseDayAfterShortMonthAndCatchup(t *testing.T) {
	anchor := time.Date(2025, time.January, 31, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		after time.Time
		want  time.Time
	}{
		{time.Date(2025, time.February, 28, 12, 0, 0, 0, time.UTC), time.Date(2025, time.March, 31, 12, 0, 0, 0, time.UTC)},
		{time.Date(2025, time.May, 15, 0, 0, 0, 0, time.UTC), time.Date(2025, time.May, 31, 12, 0, 0, 0, time.UTC)},
	} {
		got := nextTrafficResetAfter(anchor, 2, test.after)
		if got == nil || !got.Equal(test.want) {
			t.Fatalf("next reset after %v = %v, want %v", test.after, got, test.want)
		}
	}
}

func TestTrafficResetCalendarAndLeapDaySchedules(t *testing.T) {
	anchor := time.Date(2024, time.February, 29, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		policy int16
		after  time.Time
		want   time.Time
	}{
		{1, time.Date(2025, time.February, 28, 0, 0, 0, 0, time.UTC), time.Date(2025, time.March, 1, 0, 0, 0, 0, time.UTC)},
		{3, time.Date(2025, time.December, 31, 0, 0, 0, 0, time.UTC), time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)},
		{4, time.Date(2025, time.February, 28, 12, 0, 0, 0, time.UTC), time.Date(2026, time.February, 28, 12, 0, 0, 0, time.UTC)},
		{4, time.Date(2028, time.February, 29, 12, 0, 0, 0, time.UTC), time.Date(2029, time.February, 28, 12, 0, 0, 0, time.UTC)},
	} {
		got := nextTrafficResetAfter(anchor, test.policy, test.after)
		if got == nil || !got.Equal(test.want) {
			t.Fatalf("policy %d next reset after %v = %v, want %v", test.policy, test.after, got, test.want)
		}
	}
}

func TestTrafficResetRepairsMissingScheduleWithoutEarlyGrant(t *testing.T) {
	f := newOrderFixture(t)
	if err := f.h.db.Model(&f.planRecord).Update("reset_policy", 2).Error; err != nil {
		t.Fatal(err)
	}
	paid := f.paid(t, f.create(t, 0).ID)
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).
		Update("next_reset_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := f.h.runDueTrafficResets(now); err != nil {
		t.Fatal(err)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if sub.NextResetAt == nil || !sub.NextResetAt.After(now) || sub.CycleStartUsed != 0 {
		t.Fatalf("missing reset schedule repair = %+v", sub)
	}
	var resetEvents int64
	if err := f.h.db.Model(&model.QuotaEvent{}).Where("subscription_id = ? AND event_type = ?", sub.ID, "reset").Count(&resetEvents).Error; err != nil {
		t.Fatal(err)
	}
	if resetEvents != 0 {
		t.Fatalf("early reset events = %d", resetEvents)
	}
}

func TestAddonPurchasedAtDueBoundaryAppliesAfterReset(t *testing.T) {
	f := newOrderFixture(t)
	if err := f.h.db.Model(&f.planRecord).Update("reset_policy", 1).Error; err != nil {
		t.Fatal(err)
	}
	paid := f.paid(t, f.create(t, 0).ID)
	now := time.Now().UTC()
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Updates(map[string]any{
		"flow_total": 1536, "flow_used": 800, "next_reset_at": now.Add(-time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	addon := model.PlanSKU{PlanID: f.planRecord.ID, Code: "reset-addon", Name: "Addon",
		SKUType: "traffic_pack", BillingMode: "one_time", EntitlementMode: "traffic_addon",
		BillingUnit: "once", BillingValue: 1, TrafficBytes: 512, PriceCents: 100, Currency: "CNY", IsActive: true}
	if err := f.h.db.Create(&addon).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.PlanSKUOperation{PlanSKUID: addon.ID, Operation: skuOperationAddon}).Error; err != nil {
		t.Fatal(err)
	}
	f.skuRecord = addon
	order := f.create(t, paid.SubscriptionID)
	if order.OrderType != "traffic_pack" {
		t.Fatalf("order type = %q, want traffic_pack", order.OrderType)
	}
	f.paid(t, order.ID)
	var sub model.Subscription
	if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if total, used := subscriptionCycleQuota(sub); total != 1536 || used != 0 {
		t.Fatalf("cycle quota after addon = %d/%d, want 0/1536", used, total)
	}
	if err := f.h.runDueTrafficResets(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&sub, sub.ID).Error; err != nil {
		t.Fatal(err)
	}
	if total, _ := subscriptionCycleQuota(sub); total != 1536 {
		t.Fatalf("worker discarded newly purchased addon: total %d", total)
	}
}
