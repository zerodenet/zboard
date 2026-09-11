package datastore

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTrafficReadIndexesCoverDimensionTimeQueries(t *testing.T) {
	want := map[string]string{
		"idx_traffic_records_user_time":                        "(user_id, record_at)",
		"idx_traffic_records_subscription_time":                "(subscription_id, record_at)",
		"idx_traffic_records_subscription_usage":               "(subscription_id, used_bytes)",
		"idx_traffic_records_node_time":                        "(node_id, record_at)",
		"idx_traffic_records_endpoint_time":                    "(protocol_endpoint_id, record_at)",
		"idx_principal_flow_observation_user_timeline":         "(user_id, node_id, principal_key, observed_at, id)",
		"idx_principal_flow_observation_subscription_timeline": "(subscription_id, node_id, principal_key, observed_at, id)",
		"idx_principal_flow_scope_boundary_timeline":           "(scope_type, scope_id, source, node_id, observed_at, id)",
		"idx_principal_flow_current_endpoint_usage":            "(protocol_endpoint_id, active_flows, user_id, observed_at)",
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

func TestSQLiteTrafficTotalsUseCoveringIndexAfterUpgrade(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "totals.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := db.Exec(`CREATE TABLE traffic_records (
id INTEGER PRIMARY KEY, subscription_id INTEGER NOT NULL, user_id INTEGER,
node_id INTEGER, protocol_endpoint_id INTEGER, protocol_multiplier_milli INTEGER, record_at DATETIME, used_bytes INTEGER NOT NULL, meta TEXT)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO traffic_records(id,subscription_id,used_bytes,meta) VALUES (1,1,11,'first'),(2,1,13,'second'),(3,2,17,'third')").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := ReconcileTrafficReadSchema(db); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{
		"SELECT SUM(used_bytes) FROM traffic_records WHERE subscription_id=1",
		"SELECT subscription_id,SUM(used_bytes) FROM traffic_records GROUP BY subscription_id",
	} {
		var steps []struct{ Detail string }
		if err := db.Raw("EXPLAIN QUERY PLAN " + query).Scan(&steps).Error; err != nil {
			t.Fatal(err)
		}
		covered := false
		for _, step := range steps {
			covered = covered || strings.Contains(step.Detail, "COVERING INDEX idx_traffic_records_subscription_usage")
		}
		if !covered {
			t.Fatalf("ledger totals still fetch full records: query=%s steps=%v", query, steps)
		}
	}
	var total int64
	if err := db.Raw("SELECT SUM(used_bytes) FROM traffic_records WHERE subscription_id=1").Scan(&total).Error; err != nil || total != 24 {
		t.Fatalf("upgrade changed usage: total=%d err=%v", total, err)
	}
}
