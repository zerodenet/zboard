package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
)

const quoteGB = int64(1024 * 1024 * 1024)

func preparePricedChange(t *testing.T, used int64, elapsedDays int) (orderFixture, model.Subscription, model.Order) {
	t.Helper()
	return preparePricedChangeFixture(t, newOrderFixture(t), used, elapsedDays)
}
func preparePricedChangeFixture(t *testing.T, f orderFixture, used int64, elapsedDays int) (orderFixture, model.Subscription, model.Order) {
	t.Helper()
	f.h.db.Model(&f.planRecord).Update("traffic_bytes", 100*quoteGB)
	f.h.db.Model(&f.skuRecord).Update("price_cents", 1000)
	root := f.paid(t, f.create(t, 0).ID)
	now := time.Now().UTC()
	start := now.Add(-time.Duration(elapsedDays) * 24 * time.Hour)
	end := start.Add(30 * 24 * time.Hour)
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", root.SubscriptionID).Updates(map[string]any{"start_at": start, "end_at": end, "flow_used": used}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&root).Update("paid_at", start).Error; err != nil {
		t.Fatal(err)
	}
	var sub model.Subscription
	f.h.db.First(&sub, root.SubscriptionID)
	plan := f.plan(t, 2)
	f.h.db.Model(&plan).Update("traffic_bytes", 150*quoteGB)
	sku := f.sku(t, plan.ID, 1500, "change")
	f.planRecord, f.skuRecord = plan, sku
	return f, sub, root
}
func previewPricedOrder(t *testing.T, f orderFixture, target uint) commerce.OrderPreview {
	t.Helper()
	w := httptest.NewRecorder()
	f.h.OrderPreviewHandler(w, announcementRequest(http.MethodPost, "/api/v1/orders/preview", f.token, fmt.Sprintf(`{"plan_sku_id":%d,"target_subscription_id":%d}`, f.skuRecord.ID, target)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct{ Data commerce.OrderPreview }
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}
func TestLegacyPendingPlanChangeQuoteAndPaymentUseRemainingPaidValue(t *testing.T) {
	for _, tc := range []struct {
		name   string
		used   int64
		days   int
		credit int64
	}{
		{"unused", 0, 0, 1000}, {"half time", 0, 15, 500}, {"traffic tighter", 75 * quoteGB, 15, 250},
		{"exhausted without reset SKU", 100 * quoteGB, 15, 0}, {"over quota", 110 * quoteGB, 15, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, before, _ := preparePricedChange(t, tc.used, tc.days)
			var count int64
			f.h.db.Model(&model.Order{}).Count(&count)
			quote := previewLegacyChange(t, f, before.ID)
			var afterCount int64
			f.h.db.Model(&model.Order{}).Count(&afterCount)
			if count != afterCount {
				t.Fatal("preview created an order")
			}
			if quote.CreditAmount != tc.credit || quote.PayableAmount != 1500-tc.credit || quote.TrafficBytes != 150*quoteGB || !quote.EndAt.Equal(before.EndAt) {
				t.Fatal(quote)
			}
			order := createLegacyChange(t, f, before.ID)
			if order.DiscountAmount != tc.credit || order.PayableAmount != quote.PayableAmount {
				t.Fatal(order)
			}
			var pending model.Subscription
			f.h.db.First(&pending, before.ID)
			if pending.PlanID != before.PlanID || pending.FlowUsed != before.FlowUsed || pending.FlowTotal != before.FlowTotal || !pending.EndAt.Equal(before.EndAt) {
				t.Fatal("pending change granted entitlement", pending)
			}
			paid := f.paid(t, order.ID)
			var after model.Subscription
			f.h.db.First(&after, before.ID)
			if paid.PaidAmount != quote.PayableAmount || after.FlowTotal != 150*quoteGB || after.FlowUsed != before.FlowUsed || !after.EndAt.Equal(before.EndAt) || after.PlanID != 2 {
				t.Fatal(paid, after)
			}
			// Repeated callback cannot grant quota or transferred credit again.
			f.paid(t, order.ID)
			var events int64
			f.h.db.Model(&model.QuotaEvent{}).Where("event_type = ? AND reference_id = ?", "plan_change", fmt.Sprint(order.ID)).Count(&events)
			if events != 1 {
				t.Fatal("duplicate plan-change grant", events)
			}
		})
	}
}
func TestLegacyPendingPlanChangeRejectsChangedQuoteAndReusedSource(t *testing.T) {
	f, sub, _ := preparePricedChange(t, 0, 0)
	quote := previewLegacyChange(t, f, sub.ID)
	f.h.db.Model(&model.Subscription{}).Where("id = ?", sub.ID).Update("flow_used", 10*quoteGB)
	_, err := f.h.services.OrderCreation.Create(context.Background(), 1, commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, TargetSubscriptionID: sub.ID, QuoteFingerprint: quote.QuoteFingerprint})
	if err == nil {
		t.Fatal("stale quote accepted")
	}
	first := createLegacyChange(t, f, sub.ID)
	second := createLegacyChange(t, f, sub.ID)
	f.paid(t, first.ID)
	if response := f.pay(t, second.ID, false); response.Code != 400 {
		t.Fatal("spent source accepted", response.Code, response.Body.String())
	}
	var pending model.Order
	f.h.db.First(&pending, second.ID)
	if pending.Status != "pending" {
		t.Fatal("rejected settlement changed order", pending)
	}
}
func TestLegacyPendingPlanChangeCarriesOnlyPaidAndTransferredValue(t *testing.T) {
	f, sub, _ := preparePricedChange(t, 40*quoteGB, 0)
	first := f.paid(t, createLegacyChange(t, f, sub.ID).ID)
	if first.DiscountAmount != 600 || first.PaidAmount != 900 {
		t.Fatal(first)
	}
	plan := f.plan(t, 3)
	f.h.db.Model(&plan).Update("traffic_bytes", 200*quoteGB)
	sku := f.sku(t, plan.ID, 2000, "change")
	f.planRecord, f.skuRecord = plan, sku
	quote := previewLegacyChange(t, f, sub.ID)
	// B's value is 900 paid + 600 transferred, not old A + full B again.
	if quote.TrafficCredit != 1100 || quote.CreditAmount != 1100 || quote.PayableAmount != 900 {
		t.Fatal(quote)
	}
}

func TestLegacyPendingExhaustedPlanChangeCandidatesAndSettlementUseSameEligibility(t *testing.T) {
	for _, status := range []string{"active", "expired"} {
		t.Run(status, func(t *testing.T) {
			f := newOrderAccessFixture(t)
			endpoint := attachOrderPublishEndpoint(t, f)
			f, sub, root := preparePricedChangeFixture(t, f, 100*quoteGB, 15)
			if err := f.h.db.Model(&sub).Update("status", status).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.h.db.Model(&model.ProtocolCredential{}).Where("subscription_id = ?", sub.ID).Update("status", "expired").Error; err != nil {
				t.Fatal(err)
			}
			if response, access := orderAccess(t, f, root.ID, f.token); response.Code != 200 || access.Configured {
				t.Fatal("exhausted subscription exposed usable access", response.Code, access)
			}
			var page struct {
				Items []adminSubscriptionListItem
				Total int
			}
			if code := f.get(t, "/api/v1/subscriptions?paged=true&eligible_for=change&limit=1", f.h.SubscriptionsHandler, &page); code != 200 || page.Total != 1 || len(page.Items) != 1 || !page.Items[0].CanChange || page.Items[0].CanReset {
				t.Fatal("candidate/operation mismatch", code, page)
			}
			quote := previewLegacyChange(t, f, sub.ID)
			if quote.CreditAmount != 0 || quote.PayableAmount != 1500 || quote.UsedBytes != sub.FlowUsed {
				t.Fatal("exhausted entitlement had transferable value", quote)
			}
			order := createLegacyChange(t, f, sub.ID)
			if response, access := orderAccess(t, f, order.ID, f.token); response.Code != 200 || access.Configured {
				t.Fatal("pending change exposed usable access", response.Code, access)
			}
			f.paid(t, order.ID)
			var after model.Subscription
			f.h.db.First(&after, sub.ID)
			if after.Status != "active" || after.PlanID != 2 || after.FlowUsed != sub.FlowUsed || after.FlowTotal != 150*quoteGB || !after.EndAt.Equal(sub.EndAt) {
				t.Fatal("change reset or extended the exhausted cycle", after)
			}
			var credential model.ProtocolCredential
			if err := f.h.db.Where("subscription_id = ? AND protocol_endpoint_id = ? AND status = ?", sub.ID, endpoint.ID, "active").First(&credential).Error; err != nil {
				t.Fatal("paid change did not restore credentials", err)
			}
			if response, access := orderAccess(t, f, order.ID, f.token); response.Code != 200 || !access.Configured || access.SubscriptionURL == "" {
				t.Fatal("paid change did not restore subscription access", response.Code, access)
			}
			var publication model.NodeConfigPublish
			if err := f.h.db.First(&publication, endpoint.NodeID).Error; err != nil || publication.Generation != 2 {
				t.Fatal("paid change did not persist node publication", publication, err)
			}
			f.paid(t, order.ID)
			f.h.db.First(&publication, endpoint.NodeID)
			if publication.Generation != 2 {
				t.Fatal("replayed change republished entitlement", publication)
			}
		})
	}
}

func TestLegacyPendingPlanChangeAdministratorOverrideTransfersOnlyAppliedCredit(t *testing.T) {
	f, sub, _ := preparePricedChange(t, 0, 0)
	quote := previewLegacyChange(t, f, sub.ID)
	// An assignment already persisted before full-term checkout keeps the
	// administrator's original agreed amount; it is not a new API checkout.
	order := createLegacyChange(t, f, sub.ID)
	if err := f.h.db.Model(&order).Updates(map[string]any{"payable_amount": int64(1500), "discount_amount": int64(0), "assigned_by": uint(1)}).Error; err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal([]byte(order.ChangeSnapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot["Credit"] = int64(0)
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&order).Update("change_snapshot", string(payload)).Error; err != nil {
		t.Fatal(err)
	}
	_ = quote
	paid := f.paid(t, order.ID)
	if paid.DiscountAmount != 0 || paid.PaidAmount != 1500 {
		t.Fatal(paid)
	}
	plan := f.plan(t, 3)
	f.h.db.Model(&plan).Update("traffic_bytes", 200*quoteGB)
	sku := f.sku(t, plan.ID, 2000, "change")
	f.planRecord, f.skuRecord = plan, sku
	next := previewLegacyChange(t, f, sub.ID)
	if next.CreditAmount != 1500 || next.PayableAmount != 500 {
		t.Fatal("unapplied old credit inflated value", next)
	}
}

func TestLegacyPendingPlanChangeWaitingForPaymentLocksTimeButRechecksConsumedAndRefundedValue(t *testing.T) {
	for _, tc := range []struct {
		name   string
		used   int64
		refund int64
		paid   bool
	}{
		{"waiting alone", 0, 0, true},
		{"usage within locked time credit", 40 * quoteGB, 0, true},
		{"usage consumes credited value", 60 * quoteGB, 0, false},
		{"source was refunded", 0, 600, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, sub, root := preparePricedChange(t, 0, 15)
			order := createLegacyChange(t, f, sub.ID)
			if err := f.h.db.First(&order, order.ID).Error; err != nil {
				t.Fatal(err)
			}
			// Move the whole quoted timeline back one day to simulate a payment
			// delay without sleeping or changing its relative quoted entitlement.
			var snapshot map[string]json.RawMessage
			if err := json.Unmarshal([]byte(order.ChangeSnapshot), &snapshot); err != nil {
				t.Fatal(err)
			}
			var quotedAt time.Time
			if err := json.Unmarshal(snapshot["QuotedAt"], &quotedAt); err != nil {
				t.Fatal(err)
			}
			sub.StartAt, sub.EndAt = sub.StartAt.Add(-24*time.Hour), sub.EndAt.Add(-24*time.Hour)
			snapshot["QuotedAt"], _ = json.Marshal(quotedAt.Add(-24 * time.Hour))
			snapshot["EndAt"], _ = json.Marshal(sub.EndAt)
			payload, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.h.db.Model(&order).Updates(map[string]any{"change_snapshot": string(payload), "created_at": order.CreatedAt.Add(-24 * time.Hour)}).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.h.db.Model(&root).Updates(map[string]any{"paid_at": sub.StartAt, "refund_amount": tc.refund}).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.h.db.Model(&sub).Updates(map[string]any{"start_at": sub.StartAt, "end_at": sub.EndAt, "flow_used": tc.used}).Error; err != nil {
				t.Fatal(err)
			}
			response := f.pay(t, order.ID, true)
			if tc.paid && response.Code != http.StatusOK || !tc.paid && response.Code != http.StatusBadRequest {
				t.Fatal(response.Code, response.Body.String())
			}
			var after model.Subscription
			f.h.db.First(&after, sub.ID)
			var stored model.Order
			f.h.db.First(&stored, order.ID)
			if tc.paid {
				if stored.PaidAmount != order.PayableAmount || stored.DiscountAmount != order.DiscountAmount || after.FlowUsed != tc.used || after.FlowTotal != 150*quoteGB || !after.EndAt.Equal(sub.EndAt) {
					t.Fatal("agreed terms changed at payment", stored, after)
				}
			} else if stored.Status != "pending" || after.PlanID != sub.PlanID || after.FlowTotal != sub.FlowTotal {
				t.Fatal("changed credit was granted", stored, after)
			}
		})
	}
}
