package handler

import (
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestRenewalUsesPurchasedBasePriceAndPreservesHistory(t *testing.T) {
	assertRenewalPriceLock(t, newOrderFixture(t))
}
func TestMySQLRenewalUsesPurchasedBasePriceAndPreservesHistory(t *testing.T) {
	assertRenewalPriceLock(t, newMySQLOrderFixture(t))
}
func assertRenewalPriceLock(t *testing.T, f orderFixture) {
	t.Helper()
	if err := f.h.db.Model(&f.skuRecord).Updates(map[string]any{"price_cents": 12000, "renewal_effect": "extend_only"}).Error; err != nil {
		t.Fatal(err)
	}
	initial := f.create(t, 0)
	// A first-order discount is a separate fact. It cannot replace the basic
	// agreement or be converted to transferable cash by this lock-price change.
	if err := f.h.db.Model(&initial).Updates(map[string]any{"discount_amount": 2000, "payable_amount": 10000}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&f.skuRecord).Update("price_cents", 20000).Error; err != nil {
		t.Fatal(err)
	}
	initial = f.paid(t, initial.ID)
	var sub model.Subscription
	if err := f.h.db.First(&sub, initial.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if sub.RenewalPriceMinor != 12000 || initial.PaidAmount != 10000 {
		t.Fatalf("catalog price or discount changed the base agreement: base=%d paid=%d", sub.RenewalPriceMinor, initial.PaidAmount)
	}
	// Use the real calendar schedule while changing only consumption.
	if err := f.h.db.Model(&sub).Update("flow_used", 60).Error; err != nil {
		t.Fatal(err)
	}
	request := commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, TargetSubscriptionID: sub.ID}
	quote, err := f.h.services.OrderCreation.Preview(t.Context(), 1, request)
	if err != nil || quote.AmountCents != 12000 || quote.PayableAmount != 12000 || quote.QuoteFingerprint == "" {
		t.Fatalf("renewal quote: %+v %v", quote, err)
	}
	request.QuoteFingerprint = quote.QuoteFingerprint
	originalEnd := sub.EndAt
	nextEnd := originalEnd.AddDate(0, 0, 1)
	if err := f.h.db.Model(&sub).Update("end_at", nextEnd).Error; err != nil {
		t.Fatal(err)
	}
	var invalid *commerce.ValidationError
	if _, err := f.h.services.OrderCreation.Create(t.Context(), 1, request); !errors.As(err, &invalid) {
		t.Fatalf("changed renewal quote accepted: %v", err)
	}
	if err := f.h.db.Model(&sub).Update("end_at", originalEnd).Error; err != nil {
		t.Fatal(err)
	}
	renewal, err := f.h.services.OrderCreation.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	if renewal.AmountCents != 12000 || renewal.PayableAmount != 12000 {
		t.Fatalf("renewal created at live price: %+v", renewal)
	}
	if err := f.h.db.Model(&f.skuRecord).Update("price_cents", 30000).Error; err != nil {
		t.Fatal(err)
	}
	f.paid(t, renewal.ID)
	var renewed model.Subscription
	if err := f.h.db.First(&renewed, sub.ID).Error; err != nil {
		t.Fatal(err)
	}
	if renewed.RenewalPriceMinor != 12000 || renewed.FlowUsed != 60 || renewed.FlowTotal != sub.FlowTotal || !renewed.EndAt.Equal(sub.EndAt.AddDate(0, 1, 0)) {
		t.Fatalf("renewal changed price, quota or term: %+v", renewed)
	}
	if (renewed.NextResetAt == nil) != (sub.NextResetAt == nil) || (sub.NextResetAt != nil && !renewed.NextResetAt.Equal(*sub.NextResetAt)) {
		t.Fatal("renewal moved the scheduled reset")
	}
	var original model.Order
	if err := f.h.db.First(&original, initial.ID).Error; err != nil || original.AmountCents != 12000 || original.DiscountAmount != 2000 || original.PayableAmount != 10000 || original.PaidAmount != 10000 {
		t.Fatalf("purchase history rewritten: %+v %v", original, err)
	}
	other := f.paid(t, f.create(t, 0).ID)
	otherQuote, err := f.h.services.OrderCreation.Preview(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, TargetSubscriptionID: other.SubscriptionID})
	if err != nil || otherQuote.AmountCents != 30000 {
		t.Fatalf("independent subscription borrowed an older lock: %+v %v", otherQuote, err)
	}
}

func TestRenewalExplainsMissingHistoricalPriceAndQuotesNewPeriod(t *testing.T) {
	f := newOrderFixture(t)
	legacy := model.Subscription{UserID: 1, PlanID: f.planRecord.ID, PlanSKUID: f.skuRecord.ID, NodeGroupID: f.group.ID, StartAt: time.Now().UTC(), EndAt: time.Now().UTC().Add(time.Hour), Status: "active", FlowTotal: 1024, RenewalPriceMinor: 100, Config: "{}"}
	if err := f.h.db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	var invalid *commerce.ValidationError
	_, err := f.h.services.OrderCreation.Preview(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, TargetSubscriptionID: legacy.ID})
	if !errors.As(err, &invalid) {
		t.Fatalf("current catalog fabricated historical terms: %v", err)
	}
	paid := f.paid(t, f.create(t, 0).ID)
	if err := f.h.db.Model(&f.skuRecord).Update("billing_unit", "year").Error; err != nil {
		t.Fatal(err)
	}
	quote, err := f.h.services.OrderCreation.Preview(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, TargetSubscriptionID: paid.SubscriptionID})
	if err != nil || !quote.RequiresPriceAcceptance || quote.PriceBasis != "current_catalog" || quote.AmountCents != f.skuRecord.PriceCents {
		t.Fatalf("new period must use an explicit current quote: %+v %v", quote, err)
	}
}

func TestRenewalNewPeriodRequiresAcceptanceAndKeepsBothAgreements(t *testing.T) {
	assertRenewalNewPeriodAgreement(t, newOrderFixture(t))
}
func TestMySQLRenewalNewPeriodRequiresAcceptanceAndKeepsBothAgreements(t *testing.T) {
	assertRenewalNewPeriodAgreement(t, newMySQLOrderFixture(t))
}
func assertRenewalNewPeriodAgreement(t *testing.T, f orderFixture) {
	t.Helper()
	if err := f.h.db.Model(&f.skuRecord).Update("price_cents", 1200).Error; err != nil {
		t.Fatal(err)
	}
	paid := f.paid(t, f.create(t, 0).ID)
	if err := f.h.db.Model(&f.skuRecord).Updates(map[string]any{"billing_unit": "year", "price_cents": 10000}).Error; err != nil {
		t.Fatal(err)
	}
	request := commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, TargetSubscriptionID: paid.SubscriptionID}
	quote, err := f.h.services.OrderCreation.Preview(t.Context(), 1, request)
	if err != nil || quote.AmountCents != 10000 || !quote.RequiresPriceAcceptance {
		t.Fatalf("unbought year inferred from month: %+v %v", quote, err)
	}
	var invalid *commerce.ValidationError
	if _, err := f.h.services.OrderCreation.Create(t.Context(), 1, request); !errors.As(err, &invalid) {
		t.Fatalf("new price accepted without confirmation: %v", err)
	}
	request.QuoteFingerprint = quote.QuoteFingerprint
	if err := f.h.db.Model(&f.skuRecord).Update("price_cents", 11000).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.OrderCreation.Create(t.Context(), 1, request); !errors.As(err, &invalid) {
		t.Fatalf("stale annual quote accepted: %v", err)
	}
	request.QuoteFingerprint = ""
	quote, err = f.h.services.OrderCreation.Preview(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	request.QuoteFingerprint = quote.QuoteFingerprint
	renewal, err := f.h.services.OrderCreation.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&f.skuRecord).Update("price_cents", 18000).Error; err != nil {
		t.Fatal(err)
	}
	f.paid(t, renewal.ID)
	request.QuoteFingerprint = ""
	agreed, err := f.h.services.OrderCreation.Preview(t.Context(), 1, request)
	if err != nil || agreed.AmountCents != 11000 || agreed.RequiresPriceAcceptance || agreed.PriceBasis != "purchased_period" {
		t.Fatalf("actual annual agreement lost: %+v %v", agreed, err)
	}
	if err := f.h.db.Model(&f.skuRecord).Updates(map[string]any{"billing_unit": "month", "price_cents": 2400}).Error; err != nil {
		t.Fatal(err)
	}
	monthly, err := f.h.services.OrderCreation.Preview(t.Context(), 1, request)
	if err != nil || monthly.AmountCents != 1200 || monthly.RequiresPriceAcceptance {
		t.Fatalf("annual agreement replaced original monthly lock: %+v %v", monthly, err)
	}
	independent := f.paid(t, f.create(t, 0).ID)
	other, err := f.h.services.OrderCreation.Preview(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, TargetSubscriptionID: independent.SubscriptionID})
	if err != nil || other.AmountCents != 2400 {
		t.Fatalf("independent price agreement mixed: %+v %v", other, err)
	}
}
