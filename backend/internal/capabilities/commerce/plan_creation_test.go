package commerce

import (
	"errors"
	"testing"
)

func TestPlanCreationRequiresRealPurchaseAvailability(t *testing.T) {
	no := false
	yes := true
	input := PlanCreateRequest{Name: "Product", Slug: "product", NodeGroupID: 1, TrafficBytes: 1024, DeviceLimit: 2, IsActive: true, SKUs: []SKURequest{{Code: "sku", Name: "SKU", BillingUnit: "month", BillingValue: 1, Currency: "CNY", IsActive: &no}}}
	_, err := NormalizePlanCreation(input)
	var invalid *ValidationError
	if !errors.As(err, &invalid) || invalid.Fields["skus"] == "" {
		t.Fatalf("inactive SKU counted: %v", err)
	}
	input.SKUs[0].IsActive = &yes
	input.SKUs[0].AllowedOperations = []string{"renew"}
	if _, err := NormalizePlanCreation(input); !errors.As(err, &invalid) {
		t.Fatalf("renew-only counted: %v", err)
	}
	input.SKUs[0].AllowedOperations = []string{"purchase"}
	value, err := NormalizePlanCreation(input)
	if err != nil || !value.Plan.IsActive {
		t.Fatalf("purchase SKU: %+v %v", value, err)
	}
}
func TestPlanCreationRejectsNormalizedDuplicateCodes(t *testing.T) {
	input := PlanCreateRequest{Name: "Product", Slug: "product", NodeGroupID: 1, TrafficBytes: 1024, DeviceLimit: 2, SKUs: []SKURequest{{Code: " SKU ", Name: "One", BillingUnit: "month", BillingValue: 1, Currency: "CNY"}, {Code: "sku", Name: "Two", BillingUnit: "month", BillingValue: 1, Currency: "CNY"}}}
	_, err := NormalizePlanCreation(input)
	var conflict *IdentifierConflict
	if !errors.As(err, &conflict) || conflict.Fields["skus.1.code"] == "" {
		t.Fatalf("duplicate contract: %v", err)
	}
	if input.SKUs[0].Code != " SKU " {
		t.Fatal("normalization modified request")
	}
}
