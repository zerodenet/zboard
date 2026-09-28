package commerce

import (
	"math"
	"testing"
	"time"
)

func TestRemainingCreditCombinesTimeAndTraffic(t *testing.T) {
	start := time.Unix(100000, 0)
	end := start.Add(30 * 24 * time.Hour)
	for _, tc := range []struct {
		name                                 string
		elapsed                              time.Duration
		used, wantTime, wantFlow, wantCredit int64
	}{
		{"unused", 0, 0, 1000, 1000, 1000},
		{"half time", 15 * 24 * time.Hour, 0, 500, 1000, 500},
		{"traffic limits credit", 15 * 24 * time.Hour, 75, 500, 250, 250},
		{"exhausted", 15 * 24 * time.Hour, 100, 500, 0, 0},
		{"expired", 30 * 24 * time.Hour, 0, 0, 1000, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tm, flow := RemainingCredit(1000, start, end, start.Add(tc.elapsed), 100, tc.used)
			if tm != tc.wantTime || flow != tc.wantFlow || min(tm, flow) != tc.wantCredit {
				t.Fatalf("time=%d flow=%d", tm, flow)
			}
		})
	}
	if got := proportionalValue(math.MaxInt64, math.MaxInt64/2, math.MaxInt64); got != math.MaxInt64/2 {
		t.Fatal("overflow in valuation", got)
	}
}
func TestTrafficResetSKURequiresResetOnlyWithNoOverrides(t *testing.T) {
	request := SKURequest{Code: "reset", Name: "重置流量", BillingMode: "one_time", EntitlementMode: "traffic_reset", AllowedOperations: []string{"reset"}, BillingUnit: "once", BillingValue: 1, PriceCents: 500, Currency: "CNY"}
	sku, err := NormalizeSKU(1, request)
	if err != nil || sku.SKU.SKUType != "traffic_reset" || sku.RenewalEffect != "none" || sku.SKU.TrafficBytes != 0 {
		t.Fatal(sku, err)
	}
	target := &OrderTarget{ID: 1, PlanID: 1}
	if kind, err := DeriveOrderType(1, sku.EntitlementMode, sku.AllowedOperations, target); err != nil || kind != "traffic_reset" {
		t.Fatal(kind, err)
	}
	if _, err := DeriveOrderType(2, sku.EntitlementMode, sku.AllowedOperations, target); err == nil {
		t.Fatal("cross-plan reset accepted")
	}
	if _, err := DeriveOrderType(1, sku.EntitlementMode, sku.AllowedOperations, nil); err == nil {
		t.Fatal("new reset subscription accepted")
	}
	for _, mutate := range []func(*SKURequest){
		func(r *SKURequest) { r.AllowedOperations = []string{"purchase", "reset"} },
		func(r *SKURequest) { r.BillingUnit = "month" },
		func(r *SKURequest) { r.BillingMode = "periodic" },
		func(r *SKURequest) { r.GrantTrafficBytes = 100 },
		func(r *SKURequest) { r.BillingValue = 2 },
	} {
		invalid := request
		mutate(&invalid)
		if _, err := NormalizeSKU(1, invalid); err == nil {
			t.Fatal("invalid reset SKU accepted", invalid)
		}
	}
}
