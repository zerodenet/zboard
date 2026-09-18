package commerce

import "testing"

func TestDeriveOrderTypeForSKU(t *testing.T) {
	operations := []string{skuOperationPurchase, skuOperationRenew, skuOperationChange}

	orderType, err := DeriveOrderType(10, skuEntitlementPlan, operations, nil)
	if err != nil || orderType != "new" {
		t.Fatalf("derive purchase: type=%q err=%v", orderType, err)
	}

	samePlan := &OrderTarget{PlanID: 10}
	orderType, err = DeriveOrderType(10, skuEntitlementPlan, operations, samePlan)
	if err != nil || orderType != "renewal" {
		t.Fatalf("derive renewal: type=%q err=%v", orderType, err)
	}

	otherPlan := &OrderTarget{PlanID: 9}
	orderType, err = DeriveOrderType(10, skuEntitlementPlan, operations, otherPlan)
	if err != nil || orderType != "upgrade" {
		t.Fatalf("derive plan change: type=%q err=%v", orderType, err)
	}

	orderType, err = DeriveOrderType(10, skuEntitlementTrafficAddon, []string{skuOperationAddon}, samePlan)
	if err != nil || orderType != "traffic_pack" {
		t.Fatalf("derive addon: type=%q err=%v", orderType, err)
	}
}

func TestDeriveOrderTypeRejectsUnsupportedContext(t *testing.T) {
	if _, err := DeriveOrderType(10, skuEntitlementPlan, []string{skuOperationRenew}, nil); err == nil {
		t.Fatal("expected purchase rejection")
	}
	if _, err := DeriveOrderType(10, skuEntitlementPlan, []string{skuOperationPurchase}, &OrderTarget{PlanID: 10}); err == nil {
		t.Fatal("expected renewal rejection")
	}
}

func TestOrderTermsSnapshotSeparatesPriceAndEntitlementSources(t *testing.T) {
	plan := Plan{ID: 10, Name: "套餐", IsRenewable: true, TrafficBytes: 1000, DeviceLimit: 3, SpeedLimitMbps: 50}
	sku := SKU{Name: "月付", PriceCents: 123, Currency: "CNY", BillingUnit: "month", BillingValue: 1, RenewalEffect: "extend_only", TrafficBytes: 20, DeviceLimit: 99, SpeedLimitMbps: 99}
	target := &OrderTarget{ID: 7, PlanID: 10}
	cases := []struct {
		name, mode, operation, orderType string
		target                           *OrderTarget
		traffic                          int64
		devices, speed                   int
	}{
		{"purchase", "plan", "purchase", "new", nil, 1000, 3, 50},
		{"renew", "plan", "renew", "renewal", target, 1000, 3, 50},
		{"change", "plan", "change", "upgrade", &OrderTarget{ID: 8, PlanID: 9}, 1000, 3, 50},
		{"addon", "traffic_addon", "addon", "traffic_pack", target, 20, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			terms, err := SnapshotOrderTerms(plan, sku, tc.mode, []string{tc.operation}, tc.target, OrderCreateRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if terms.OrderType != tc.orderType || terms.TrafficBytes != tc.traffic || terms.DeviceLimit != tc.devices || terms.SpeedLimitMbps != tc.speed {
				t.Fatalf("wrong entitlement snapshot: %+v", terms)
			}
			if terms.AmountCents != 123 || terms.PayableAmount != 123 || terms.Currency != "CNY" || terms.PlanName != plan.Name || terms.SKUName != sku.Name || terms.BillingUnit != "month" || terms.BillingValue != 1 || terms.RenewalEffect != "extend_only" || terms.Channel != "manual" {
				t.Fatalf("wrong commercial snapshot: %+v", terms)
			}
			if tc.target == nil {
				if terms.TargetSubscriptionID != nil {
					t.Fatal("purchase has target")
				}
			} else if terms.TargetSubscriptionID == nil || *terms.TargetSubscriptionID != tc.target.ID {
				t.Fatal("target lost")
			}
		})
	}
	terms, err := SnapshotOrderTerms(plan, sku, "plan", []string{"renew"}, target, OrderCreateRequest{OrderType: " RENEWAL ", Channel: " admin_assignment "})
	if err != nil || terms.Channel != "admin_assignment" {
		t.Fatalf("compatibility assertion: %+v %v", terms, err)
	}
	target.ID = 99
	if *terms.TargetSubscriptionID != 7 {
		t.Fatal("snapshot aliases mutable target")
	}
}

func TestOrderTermsRejectsOverrideAndNonRenewablePlan(t *testing.T) {
	plan := Plan{ID: 10}
	for _, tc := range []struct {
		name    string
		target  *OrderTarget
		request OrderCreateRequest
		field   string
	}{
		{"override", nil, OrderCreateRequest{OrderType: "renewal"}, "order_type"},
		{"renewal disabled", &OrderTarget{ID: 7, PlanID: 10}, OrderCreateRequest{}, "plan_sku_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			terms, err := SnapshotOrderTerms(plan, SKU{}, "plan", []string{"purchase", "renew"}, tc.target, tc.request)
			validation, ok := err.(*ValidationError)
			if !ok || validation.Fields[tc.field] == "" {
				t.Fatalf("expected %s failure, got %v", tc.field, err)
			}
			if terms != (OrderTerms{}) {
				t.Fatal("rejected order returned usable terms")
			}
		})
	}
}
