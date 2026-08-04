package handler

import (
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func validCommerceSKURequest() commercePlanSKURequest {
	active := true
	return commercePlanSKURequest{
		Code: "starter-monthly", Name: "月付",
		BillingMode: skuBillingPeriodic,
		BillingUnit: "month", BillingValue: 1,
		PriceCents: 1000, Currency: "CNY",
		TrafficBytes: 100, DeviceLimit: 3,
		SpeedLimitMbps: 0, IsActive: &active,
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

func TestNormalizeCommercePlanSKURejectsMixedOneTimeOperations(t *testing.T) {
	request := validCommerceSKURequest()
	request.BillingMode = skuBillingOneTime
	request.BillingUnit = "once"
	request.AllowedOperations = []string{skuOperationAddon, skuOperationPurchase}

	if _, err := normalizeCommercePlanSKU(1, request); err == nil {
		t.Fatal("expected one-time operation validation error")
	}
}

func TestDeriveOrderTypeForSKU(t *testing.T) {
	operations := []string{skuOperationPurchase, skuOperationRenew, skuOperationChange}

	orderType, err := deriveOrderTypeForSKU(10, skuBillingPeriodic, operations, nil)
	if err != nil || orderType != "new" {
		t.Fatalf("derive purchase: type=%q err=%v", orderType, err)
	}

	samePlan := &model.Subscription{PlanID: 10}
	orderType, err = deriveOrderTypeForSKU(10, skuBillingPeriodic, operations, samePlan)
	if err != nil || orderType != "renewal" {
		t.Fatalf("derive renewal: type=%q err=%v", orderType, err)
	}

	otherPlan := &model.Subscription{PlanID: 9}
	orderType, err = deriveOrderTypeForSKU(10, skuBillingPeriodic, operations, otherPlan)
	if err != nil || orderType != "upgrade" {
		t.Fatalf("derive plan change: type=%q err=%v", orderType, err)
	}

	orderType, err = deriveOrderTypeForSKU(10, skuBillingOneTime, []string{skuOperationAddon}, samePlan)
	if err != nil || orderType != "traffic_pack" {
		t.Fatalf("derive addon: type=%q err=%v", orderType, err)
	}
}

func TestDeriveOrderTypeRejectsUnsupportedContext(t *testing.T) {
	if _, err := deriveOrderTypeForSKU(10, skuBillingPeriodic, []string{skuOperationRenew}, nil); err == nil {
		t.Fatal("expected purchase rejection")
	}
	if _, err := deriveOrderTypeForSKU(10, skuBillingPeriodic, []string{skuOperationPurchase}, &model.Subscription{PlanID: 10}); err == nil {
		t.Fatal("expected renewal rejection")
	}
}
