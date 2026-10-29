package handler

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
)

func TestAutomaticFiniteSQLiteReconsentAfterManualRenewalClosesOldStageImmediately(t *testing.T) {
	assertAutomaticFiniteReconsentClosesOld(t, newOrderFixture(t))
}
func TestMySQLAutomaticFiniteReconsentAfterManualRenewalClosesOldStageImmediately(t *testing.T) {
	assertAutomaticFiniteReconsentClosesOld(t, newMySQLBusinessOrderFixture(t))
}
func assertAutomaticFiniteReconsentClosesOld(t *testing.T, base orderFixture) {
	f, p := newAutomaticFiniteFixture(t, base, 50000)
	staged := stageFiniteOrder(t, f)
	oldOrder := *staged.PendingOrderID
	manual := f.buy(t, f.sub.ID, "manual-renew-before-new-consent")
	var current model.Subscription
	if err := f.db.First(&current, f.sub.ID).Error; err != nil || !current.EndAt.After(*f.at) || manual.SubscriptionID != f.sub.ID {
		t.Fatalf("manual renewal did not move expiry to future: %+v %v", current, err)
	}
	// Alternative same-period sale SKU gets a fresh explicit approval; its
	// renewal base remains the real purchased-period price, not today's catalog.
	alternative := base.sku(t, base.planRecord.ID, 25000, "renew")
	view, err := f.auto.Read(t.Context(), 1, f.sub.ID, alternative.ID)
	if err != nil || view.Offer == nil || view.Offer.PlanSKUID != alternative.ID || view.PendingOrderID == nil || *view.PendingOrderID != oldOrder {
		t.Fatalf("alternative SKU quote unavailable: %+v %v", view, err)
	}
	updated, err := f.auto.Set(t.Context(), 1, f.sub.ID, commerce.AutomaticRenewalRequest{Enabled: true, PlanSKUID: alternative.ID, QuoteFingerprint: view.Offer.QuoteFingerprint, ExpectedRevision: view.Revision})
	if err != nil || !updated.Enabled || updated.Revision <= staged.Revision || updated.PlanSKUID != alternative.ID {
		t.Fatalf("new explicit consent rejected: %+v %v", updated, err)
	}
	queued := automaticStageRecord(t, f)
	if queued.PendingOrderID == nil || *queued.PendingOrderID != oldOrder || queued.PendingConsentRevision != staged.Revision || queued.Revision == queued.PendingConsentRevision {
		t.Fatalf("old and new permissions conflated: %+v", queued)
	}
	if queued.NextAttemptAt.After(*f.at) {
		t.Fatalf("stale cleanup postponed to future expiry: now=%s next=%s due=%s", *f.at, queued.NextAttemptAt, current.EndAt)
	}
	calls := f.quotes.calls.Load()
	if n, err := f.auto.Process(t.Context(), 1); n != 1 || err != nil {
		t.Fatalf("old stage not immediately canceled: %d %v", n, err)
	}
	cleaned := automaticStageRecord(t, f)
	if cleaned.PendingOrderID != nil || !cleaned.Enabled || cleaned.PlanSKUID != alternative.ID || cleaned.Revision != updated.Revision || cleaned.LastOrderID != nil || !cleaned.NextAttemptAt.Equal(current.EndAt) {
		t.Fatalf("cleanup changed new permission or claimed success: %+v", cleaned)
	}
	if f.quotes.calls.Load() != calls {
		t.Fatal("cancel-only cleanup requoted")
	}
	var old model.Order
	if err := f.db.Select("id,status").First(&old, oldOrder).Error; err != nil || old.Status != "canceled" {
		t.Fatalf("old order still payable: %+v %v", old, err)
	}
	var intent model.OrderDiscountRedemption
	if err := f.db.First(&intent, "order_id = ?", oldOrder).Error; err != nil || intent.Desired != "release" {
		t.Fatalf("release intent absent: %+v %v", intent, err)
	}
	automaticFiniteBalance(t, f, 30000)
	if n, err := f.auto.Process(t.Context(), 10); n != 0 || err != nil {
		t.Fatalf("future new permission charged immediately: %d %v", n, err)
	}
	if p.reserves != 2 {
		t.Fatalf("reconsent allocated an extra coupon before due: %d", p.reserves)
	}
}
