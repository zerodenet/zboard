package datastore

import "testing"

func TestCommerceOrderSnapshotColumnInventory(t *testing.T) {
	want := []string{
		"plan_name",
		"sku_name",
		"billing_unit",
		"billing_value",
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
