package handler

import "testing"

func TestHistoryRetentionDefaultsAreBoundedAndPrivate(t *testing.T) {
	defaults := historyRetentionDefaults()
	if len(defaults) != 3 {
		t.Fatalf("historyRetentionDefaults() returned %d configs, want 3", len(defaults))
	}
	values := map[string]string{}
	for _, config := range defaults {
		if config.IsPublic || config.IsSecret || config.ValueType != "int" {
			t.Fatalf("unexpected retention config metadata: %#v", config)
		}
		values[config.ConfigKey] = config.Value
	}
	if values[auditLogRetentionKey] != "180" {
		t.Fatalf("audit retention = %q, want 180", values[auditLogRetentionKey])
	}
	if values[operationRetentionKey] != "90" || values[taskRetentionKey] != "90" {
		t.Fatalf("operation/task retention = %q/%q, want 90/90", values[operationRetentionKey], values[taskRetentionKey])
	}
}
