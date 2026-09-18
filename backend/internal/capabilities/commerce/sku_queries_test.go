package commerce

import "testing"

func TestSKUProjectionPreservesLegacyFactsWithoutMutatingInput(t *testing.T) {
	operations := []string{"renew", "purchase"}
	record := SKURecord{SKU: SKU{ID: 1, BillingUnit: "once", SKUType: "new"}, Operations: operations}
	view := ProjectSKU(record)
	if view.BillingMode != "one_time" || view.EntitlementMode != "plan" || view.RenewalEffect != "add_quota_only" || view.AllowedOperations[0] != "purchase" {
		t.Fatalf("legacy plan: %+v", view)
	}
	if operations[0] != "renew" || record.SKU.BillingMode != "" {
		t.Fatal("projection modified source")
	}
	view = ProjectSKU(SKURecord{SKU: SKU{SKUType: "traffic_pack", TrafficBytes: 1024}})
	if view.GrantTrafficBytes != 1024 || view.EntitlementMode != "traffic_addon" || view.RenewalEffect != "none" || len(view.AllowedOperations) != 1 || view.AllowedOperations[0] != "addon" {
		t.Fatalf("legacy addon: %+v", view)
	}
	page := skuPage(SKURecords{})
	if page.Items == nil {
		t.Fatal("empty page must encode array")
	}
}
