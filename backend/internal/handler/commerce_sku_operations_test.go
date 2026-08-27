package handler

import (
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func validCommerceSKURequest() commercePlanSKURequest {
	active := true
	return commercePlanSKURequest{
		Code: "starter-monthly", Name: "月付",
		BillingMode: skuBillingPeriodic, EntitlementMode: skuEntitlementPlan,
		BillingUnit: "month", BillingValue: 1,
		PriceCents: 1000, Currency: "CNY",
		IsActive: &active,
	}
}

func TestNormalizeCommercePlanSKUAllowsPurchaseAndRenewal(t *testing.T) {
	request := validCommerceSKURequest()
	request.AllowedOperations = []string{skuOperationRenew, skuOperationPurchase, skuOperationRenew}

	normalized, err := normalizeCommercePlanSKU(7, request)
	if err != nil {
		t.Fatalf("normalize commerce sku: %v", err)
	}
	if normalized.SKU.PlanID != 7 {
		t.Fatalf("expected plan id 7, got %d", normalized.SKU.PlanID)
	}
	if normalized.SKU.SKUType != "new" {
		t.Fatalf("purchase-compatible sku must retain legacy new type, got %q", normalized.SKU.SKUType)
	}
	if len(normalized.AllowedOperations) != 2 || normalized.AllowedOperations[0] != skuOperationPurchase || normalized.AllowedOperations[1] != skuOperationRenew {
		t.Fatalf("unexpected normalized operations: %#v", normalized.AllowedOperations)
	}
	if normalized.RenewalEffect != skuRenewalExtendOnly {
		t.Fatalf("timed sku renewal effect = %q, want %q", normalized.RenewalEffect, skuRenewalExtendOnly)
	}
}

func TestNormalizeCommercePlanSKUMigratesLegacyRenewal(t *testing.T) {
	request := validCommerceSKURequest()
	request.SKUType = "renewal"
	request.BillingMode = ""

	normalized, err := normalizeCommercePlanSKU(1, request)
	if err != nil {
		t.Fatalf("normalize legacy renewal sku: %v", err)
	}
	if normalized.BillingMode != skuBillingPeriodic {
		t.Fatalf("expected periodic billing, got %q", normalized.BillingMode)
	}
	if len(normalized.AllowedOperations) != 1 || normalized.AllowedOperations[0] != skuOperationRenew {
		t.Fatalf("unexpected operations: %#v", normalized.AllowedOperations)
	}
}

func TestNormalizeCommercePlanSKUAllowsOneTimePlanPurchaseAndRenewal(t *testing.T) {
	request := validCommerceSKURequest()
	request.BillingMode = skuBillingOneTime
	request.BillingUnit = "month"
	request.AllowedOperations = []string{skuOperationPurchase, skuOperationRenew}

	normalized, err := normalizeCommercePlanSKU(1, request)
	if err != nil {
		t.Fatalf("normalize one-time plan sku: %v", err)
	}
	if normalized.BillingMode != skuBillingOneTime || normalized.EntitlementMode != skuEntitlementPlan {
		t.Fatalf("unexpected one-time plan metadata: %#v", normalized)
	}
	if normalized.SKU.SKUType != "new" || normalized.SKU.TrafficBytes != 0 {
		t.Fatalf("one-time plan sku must inherit plan entitlement: %#v", normalized.SKU)
	}
}

func TestNormalizeCommercePlanSKUAllowsPermanentPlanQuota(t *testing.T) {
	request := validCommerceSKURequest()
	request.BillingMode = skuBillingOneTime
	request.BillingUnit = "once"
	request.AllowedOperations = []string{skuOperationPurchase, skuOperationRenew}

	normalized, err := normalizeCommercePlanSKU(1, request)
	if err != nil {
		t.Fatalf("normalize permanent plan sku: %v", err)
	}
	if normalized.SKU.BillingUnit != "once" || normalized.EntitlementMode != skuEntitlementPlan {
		t.Fatalf("unexpected permanent plan sku: %#v", normalized)
	}
	if normalized.RenewalEffect != skuRenewalAddQuotaOnly {
		t.Fatalf("permanent renewal effect = %q, want %q", normalized.RenewalEffect, skuRenewalAddQuotaOnly)
	}
}

func TestNormalizeCommercePlanSKUAllowsExplicitTimedQuotaGrant(t *testing.T) {
	request := validCommerceSKURequest()
	request.AllowedOperations = []string{skuOperationPurchase, skuOperationRenew}
	request.RenewalEffect = skuRenewalExtendAndAdd

	normalized, err := normalizeCommercePlanSKU(1, request)
	if err != nil {
		t.Fatalf("normalize timed quota-grant renewal: %v", err)
	}
	if normalized.RenewalEffect != skuRenewalExtendAndAdd {
		t.Fatalf("renewal effect = %q", normalized.RenewalEffect)
	}
}

func TestNormalizeCommercePlanSKURejectsPermanentTimeExtension(t *testing.T) {
	request := validCommerceSKURequest()
	request.BillingMode = skuBillingOneTime
	request.BillingUnit = "once"
	request.AllowedOperations = []string{skuOperationPurchase, skuOperationRenew}
	request.RenewalEffect = skuRenewalExtendOnly

	if _, err := normalizeCommercePlanSKU(1, request); err == nil {
		t.Fatal("expected permanent renewal-effect validation error")
	}
}

func TestNormalizeCommercePlanSKUWithoutRenewalHasNoRenewalEffect(t *testing.T) {
	request := validCommerceSKURequest()
	request.AllowedOperations = []string{skuOperationPurchase}
	request.RenewalEffect = skuRenewalExtendAndAdd

	normalized, err := normalizeCommercePlanSKU(1, request)
	if err != nil {
		t.Fatalf("normalize purchase-only sku: %v", err)
	}
	if normalized.RenewalEffect != skuRenewalNone {
		t.Fatalf("purchase-only renewal effect = %q", normalized.RenewalEffect)
	}
}

func TestNormalizeCommercePlanSKURejectsPeriodicPermanentUnit(t *testing.T) {
	request := validCommerceSKURequest()
	request.BillingMode = skuBillingPeriodic
	request.BillingUnit = "once"

	if _, err := normalizeCommercePlanSKU(1, request); err == nil {
		t.Fatal("expected periodic permanent-unit validation error")
	}
}

func TestNormalizeCommercePlanSKURejectsPeriodicEntitlementOverrides(t *testing.T) {
	request := validCommerceSKURequest()
	request.TrafficBytes = 100

	if _, err := normalizeCommercePlanSKU(1, request); err == nil {
		t.Fatal("expected periodic entitlement override rejection")
	}
}

func TestNormalizeCommercePlanSKUStoresOnlyAddonGrant(t *testing.T) {
	request := validCommerceSKURequest()
	request.SKUType = "traffic_pack"
	request.BillingMode = skuBillingOneTime
	request.EntitlementMode = skuEntitlementTrafficAddon
	request.BillingUnit = "once"
	request.AllowedOperations = []string{skuOperationAddon}
	request.GrantTrafficBytes = 512

	normalized, err := normalizeCommercePlanSKU(1, request)
	if err != nil {
		t.Fatalf("normalize addon sku: %v", err)
	}
	if normalized.SKU.TrafficBytes != 512 || normalized.SKU.DeviceLimit != 0 || normalized.SKU.SpeedLimitMbps != 0 {
		t.Fatalf("unexpected addon storage: %#v", normalized.SKU)
	}
}

func TestNormalizeCommercePlanSKURejectsAddonMixedWithPlanOperations(t *testing.T) {
	request := validCommerceSKURequest()
	request.BillingMode = skuBillingOneTime
	request.EntitlementMode = skuEntitlementTrafficAddon
	request.BillingUnit = "once"
	request.GrantTrafficBytes = 100
	request.AllowedOperations = []string{skuOperationAddon, skuOperationPurchase}

	if _, err := normalizeCommercePlanSKU(1, request); err == nil {
		t.Fatal("expected traffic-addon operation validation error")
	}
}

func TestNormalizePlanPolicyDoesNotInheritSKUEntitlements(t *testing.T) {
	_, err := normalizePlanPolicy(planCreateReq{}, planSKUReq{TrafficBytes: 100, DeviceLimit: 3, SpeedLimitMbps: 50})
	if err == nil {
		t.Fatal("expected plan entitlement validation error")
	}
}

func TestDeriveOrderTypeForSKU(t *testing.T) {
	operations := []string{skuOperationPurchase, skuOperationRenew, skuOperationChange}

	orderType, err := deriveOrderTypeForSKU(10, skuEntitlementPlan, operations, nil)
	if err != nil || orderType != "new" {
		t.Fatalf("derive purchase: type=%q err=%v", orderType, err)
	}

	samePlan := &model.Subscription{PlanID: 10}
	orderType, err = deriveOrderTypeForSKU(10, skuEntitlementPlan, operations, samePlan)
	if err != nil || orderType != "renewal" {
		t.Fatalf("derive renewal: type=%q err=%v", orderType, err)
	}

	otherPlan := &model.Subscription{PlanID: 9}
	orderType, err = deriveOrderTypeForSKU(10, skuEntitlementPlan, operations, otherPlan)
	if err != nil || orderType != "upgrade" {
		t.Fatalf("derive plan change: type=%q err=%v", orderType, err)
	}

	orderType, err = deriveOrderTypeForSKU(10, skuEntitlementTrafficAddon, []string{skuOperationAddon}, samePlan)
	if err != nil || orderType != "traffic_pack" {
		t.Fatalf("derive addon: type=%q err=%v", orderType, err)
	}
}

func TestDeriveOrderTypeRejectsUnsupportedContext(t *testing.T) {
	if _, err := deriveOrderTypeForSKU(10, skuEntitlementPlan, []string{skuOperationRenew}, nil); err == nil {
		t.Fatal("expected purchase rejection")
	}
	if _, err := deriveOrderTypeForSKU(10, skuEntitlementPlan, []string{skuOperationPurchase}, &model.Subscription{PlanID: 10}); err == nil {
		t.Fatal("expected renewal rejection")
	}
}
