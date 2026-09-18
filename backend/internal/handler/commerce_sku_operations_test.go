package handler

import (
	"testing"
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

func TestNormalizePlanPolicyDoesNotInheritSKUEntitlements(t *testing.T) {
	_, err := normalizePlanPolicy(planCreateReq{}, planSKUReq{TrafficBytes: 100, DeviceLimit: 3, SpeedLimitMbps: 50})
	if err == nil {
		t.Fatal("expected plan entitlement validation error")
	}
}
