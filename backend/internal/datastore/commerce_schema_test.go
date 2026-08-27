package datastore

import "testing"

func TestRenewalEffectColumnsEvolveAfterLegacyBaselineValidation(t *testing.T) {
	for _, column := range preReleaseBaselineColumns {
		if column.column == "renewal_effect" && (column.table == "plan_skus" || column.table == "orders") {
			t.Fatalf("legacy baseline must not require evolving column %s.%s", column.table, column.column)
		}
	}

	foundOrderSnapshot := false
	for _, column := range commerceOrderSnapshotColumns {
		if column.table == "orders" && column.name == "renewal_effect" {
			foundOrderSnapshot = true
			break
		}
	}
	if !foundOrderSnapshot {
		t.Fatal("commerce reconciliation does not add orders.renewal_effect")
	}
}

func TestCommerceOrderSnapshotColumnInventory(t *testing.T) {
	want := []string{
		"plan_name",
		"sku_name",
		"billing_unit",
		"billing_value",
		"renewal_effect",
		"traffic_bytes",
		"device_limit",
		"speed_limit_mbps",
	}
	if len(commerceOrderSnapshotColumns) != len(want) {
		t.Fatalf("snapshot column count = %d, want %d", len(commerceOrderSnapshotColumns), len(want))
	}
	for index, name := range want {
		column := commerceOrderSnapshotColumns[index]
		if column.table != "orders" || column.name != name {
			t.Fatalf("snapshot column %d = %s.%s, want orders.%s", index, column.table, column.name, name)
		}
		if column.definition == "" || column.after == "" {
			t.Fatalf("snapshot column %s has incomplete DDL metadata", name)
		}
	}
}
