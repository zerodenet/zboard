package datastore

import (
	"strings"
	"testing"
)

func TestTrafficReadIndexesCoverDimensionTimeQueries(t *testing.T) {
	want := map[string]string{
		"idx_traffic_records_user_time":                        "(user_id, record_at)",
		"idx_traffic_records_subscription_time":                "(subscription_id, record_at)",
		"idx_traffic_records_node_time":                        "(node_id, record_at)",
		"idx_traffic_records_endpoint_time":                    "(protocol_endpoint_id, record_at)",
		"idx_principal_flow_observation_user_timeline":         "(user_id, node_id, principal_key, observed_at, id)",
		"idx_principal_flow_observation_subscription_timeline": "(subscription_id, node_id, principal_key, observed_at, id)",
		"idx_principal_flow_scope_boundary_timeline":           "(scope_type, scope_id, source, node_id, observed_at, id)",
	}
	if len(trafficReadIndexes) != len(want) {
		t.Fatalf("traffic read index count = %d, want %d", len(trafficReadIndexes), len(want))
	}
	for _, index := range trafficReadIndexes {
		columns, exists := want[index.name]
		if !exists {
			t.Fatalf("unexpected traffic read index %q", index.name)
		}
		if !strings.Contains(index.ddl, columns) {
			t.Errorf("index %s DDL %q does not contain ordered columns %s", index.name, index.ddl, columns)
		}
	}
}
