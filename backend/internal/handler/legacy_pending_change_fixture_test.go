package handler

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
	"time"
)

// These are pre-upgrade pending agreements, never issued by the current API.
// Their unversioned snapshot must retain the original terms at payment.
func legacyPendingChange(t *testing.T, f orderFixture, target uint) (model.Order, commerce.OrderPreview) {
	t.Helper()
	var sub model.Subscription
	var plan model.Plan
	var sku model.PlanSKU
	for _, load := range []func() error{
		func() error { return f.h.db.First(&sub, target).Error },
		func() error { return f.h.db.First(&plan, f.planRecord.ID).Error },
		func() error { return f.h.db.First(&sku, f.skuRecord.ID).Error },
	} {
		if err := load(); err != nil {
			t.Fatal(err)
		}
	}
	var history []model.Order
	if err := f.h.db.Where("subscription_id = ? AND status = ? AND order_type IN ?", target, "paid", []string{"new", "renewal", "upgrade"}).Order("id DESC").Find(&history).Error; err != nil {
		t.Fatal(err)
	}
	value := int64(0)
	start := sub.StartAt
	root := uint(0)
	for _, paid := range history {
		if paid.PlanID != sub.PlanID || paid.SubscriptionEndedAt != nil {
			break
		}
		var previous struct{ Credit int64 }
		if paid.ChangeSnapshot != "" {
			if err := json.Unmarshal([]byte(paid.ChangeSnapshot), &previous); err != nil {
				t.Fatal(err)
			}
		}
		value += max(int64(0), paid.PaidAmount-paid.RefundAmount) + max(int64(0), previous.Credit)
		root = paid.ID
		if paid.PaidAt != nil {
			start = *paid.PaidAt
		}
		if paid.OrderType == "new" || paid.OrderType == "upgrade" {
			break
		}
	}
	now := time.Now().UTC()
	_, used := entitlements.CycleQuota(entitlements.Subscription(sub))
	timeCredit, trafficCredit := commerce.RemainingCredit(value, start, sub.EndAt, now, sub.ResetQuotaBytes, used)
	credit := min(timeCredit, trafficCredit, sku.PriceCents)
	var event uint
	if err := f.h.db.Model(&model.QuotaEvent{}).Where("subscription_id = ?", target).Select("COALESCE(MAX(id),0)").Scan(&event).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := struct {
		PlanID, PlanSKUID           uint
		EndAt                       time.Time
		CycleStartUsed, ResetQuota  int64
		SourceOrderID, QuotaEventID uint
		QuotedAt                    time.Time
		Credit                      int64
	}{sub.PlanID, sub.PlanSKUID, sub.EndAt, sub.CycleStartUsed, sub.ResetQuotaBytes, root, event, now, credit}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	order := model.Order{UserID: sub.UserID, PlanID: plan.ID, PlanSKUID: sku.ID, TradeNo: uuid.NewString(), OrderType: "upgrade", TargetSubscriptionID: &target, AmountCents: sku.PriceCents, PayableAmount: sku.PriceCents - credit, DiscountAmount: credit, Currency: sku.Currency, Channel: "manual", Status: "pending", PlanName: plan.Name, SKUName: sku.Name, BillingUnit: sku.BillingUnit, BillingValue: sku.BillingValue, RenewalEffect: sku.RenewalEffect, TrafficBytes: plan.TrafficBytes, DeviceLimit: plan.DeviceLimit, SpeedLimitMbps: plan.SpeedLimitMbps, ChangeSnapshot: string(encoded)}
	preview := commerce.OrderPreview{OrderType: "upgrade", AmountCents: order.AmountCents, PayableAmount: order.PayableAmount, Currency: order.Currency, CreditAmount: credit, TimeCredit: timeCredit, TrafficCredit: trafficCredit, TrafficBytes: order.TrafficBytes, UsedBytes: used, EndAt: &sub.EndAt, QuoteFingerprint: fmt.Sprintf("historical-fixture-%d-%d", target, event)}
	return order, preview
}
func createLegacyChange(t *testing.T, f orderFixture, target uint) model.Order {
	t.Helper()
	order, _ := legacyPendingChange(t, f, target)
	if err := f.h.db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	return order
}
func previewLegacyChange(t *testing.T, f orderFixture, target uint) commerce.OrderPreview {
	t.Helper()
	_, preview := legacyPendingChange(t, f, target)
	return preview
}
