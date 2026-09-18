package commerce

import "testing"

func validSKURequest() SKURequest {
	active := true
	return SKURequest{
		Code: "starter-monthly", Name: "月付",
		BillingMode: skuBillingPeriodic, EntitlementMode: skuEntitlementPlan,
		BillingUnit: "month", BillingValue: 1,
		PriceCents: 1000, Currency: "CNY",
		IsActive: &active,
	}
}

func TestNormalizeCommercePlanSKUAllowsPurchaseAndRenewal(t *testing.T) {
	request := validSKURequest()
	request.AllowedOperations = []string{skuOperationRenew, skuOperationPurchase, skuOperationRenew}

	normalized, err := NormalizeSKU(7, request)
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
	request := validSKURequest()
	request.SKUType = "renewal"
	request.BillingMode = ""

	normalized, err := NormalizeSKU(1, request)
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
	request := validSKURequest()
	request.BillingMode = skuBillingOneTime
	request.BillingUnit = "month"
	request.AllowedOperations = []string{skuOperationPurchase, skuOperationRenew}

	normalized, err := NormalizeSKU(1, request)
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
	request := validSKURequest()
	request.BillingMode = skuBillingOneTime
	request.BillingUnit = "once"
	request.AllowedOperations = []string{skuOperationPurchase, skuOperationRenew}

	normalized, err := NormalizeSKU(1, request)
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
	request := validSKURequest()
	request.AllowedOperations = []string{skuOperationPurchase, skuOperationRenew}
	request.RenewalEffect = skuRenewalExtendAndAdd

	normalized, err := NormalizeSKU(1, request)
	if err != nil {
		t.Fatalf("normalize timed quota-grant renewal: %v", err)
	}
	if normalized.RenewalEffect != skuRenewalExtendAndAdd {
		t.Fatalf("renewal effect = %q", normalized.RenewalEffect)
	}
}

func TestNormalizeCommercePlanSKURejectsPermanentTimeExtension(t *testing.T) {
	request := validSKURequest()
	request.BillingMode = skuBillingOneTime
	request.BillingUnit = "once"
	request.AllowedOperations = []string{skuOperationPurchase, skuOperationRenew}
	request.RenewalEffect = skuRenewalExtendOnly

	if _, err := NormalizeSKU(1, request); err == nil {
		t.Fatal("expected permanent renewal-effect validation error")
	}
}

func TestNormalizeCommercePlanSKUWithoutRenewalHasNoRenewalEffect(t *testing.T) {
	request := validSKURequest()
	request.AllowedOperations = []string{skuOperationPurchase}
	request.RenewalEffect = skuRenewalExtendAndAdd

	normalized, err := NormalizeSKU(1, request)
	if err != nil {
		t.Fatalf("normalize purchase-only sku: %v", err)
	}
	if normalized.RenewalEffect != skuRenewalNone {
		t.Fatalf("purchase-only renewal effect = %q", normalized.RenewalEffect)
	}
}

func TestNormalizeCommercePlanSKURejectsPeriodicPermanentUnit(t *testing.T) {
	request := validSKURequest()
	request.BillingMode = skuBillingPeriodic
	request.BillingUnit = "once"

	if _, err := NormalizeSKU(1, request); err == nil {
		t.Fatal("expected periodic permanent-unit validation error")
	}
}

func TestNormalizeCommercePlanSKURejectsPeriodicEntitlementOverrides(t *testing.T) {
	request := validSKURequest()
	request.TrafficBytes = 100

	if _, err := NormalizeSKU(1, request); err == nil {
		t.Fatal("expected periodic entitlement override rejection")
	}
}

func TestNormalizeCommercePlanSKUStoresOnlyAddonGrant(t *testing.T) {
	request := validSKURequest()
	request.SKUType = "traffic_pack"
	request.BillingMode = skuBillingOneTime
	request.EntitlementMode = skuEntitlementTrafficAddon
	request.BillingUnit = "once"
	request.AllowedOperations = []string{skuOperationAddon}
	request.GrantTrafficBytes = 512

	normalized, err := NormalizeSKU(1, request)
	if err != nil {
		t.Fatalf("normalize addon sku: %v", err)
	}
	if normalized.SKU.TrafficBytes != 512 || normalized.SKU.DeviceLimit != 0 || normalized.SKU.SpeedLimitMbps != 0 {
		t.Fatalf("unexpected addon storage: %#v", normalized.SKU)
	}
}

func TestNormalizeCommercePlanSKURejectsAddonMixedWithPlanOperations(t *testing.T) {
	request := validSKURequest()
	request.BillingMode = skuBillingOneTime
	request.EntitlementMode = skuEntitlementTrafficAddon
	request.BillingUnit = "once"
	request.GrantTrafficBytes = 100
	request.AllowedOperations = []string{skuOperationAddon, skuOperationPurchase}

	if _, err := NormalizeSKU(1, request); err == nil {
		t.Fatal("expected traffic-addon operation validation error")
	}
}
