package datastore_test

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"reflect"
	"testing"
	"time"
)

func TestSQLiteMeteringIndexBudgetPreservesFactsAndReplayKeys(t *testing.T) {
	meteringIndexBudget(t, false)
}
func TestMySQLMeteringIndexBudgetPreservesFactsAndReplayKeys(t *testing.T) {
	meteringIndexBudget(t, true)
}
func meteringIndexBudget(t *testing.T, mysql bool) {
	db := diagnosticSchemaDB(t, mysql)
	if err := application.PrepareDatabaseSchema(db); err != nil {
		t.Fatal(err)
	}
	// Reintroduce known legacy indexes to model an upgrade, not just a fresh DB.
	for _, index := range []struct{ table, name, columns string }{{"traffic_records", "idx_traffic_user", "user_id"}, {"traffic_records", "idx_traffic_time", "record_at"}, {"flow_usages", "idx_flow_usages_principal", "principal_key"}, {"flow_usages", "idx_flow_usages_status_seen", "status,last_seen_at"}} {
		if !db.Migrator().HasIndex(index.table, index.name) {
			if err := db.Exec(fmt.Sprintf("CREATE INDEX %s ON %s(%s)", index.name, index.table, index.columns)).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	fact := model.TrafficRecord{ReportID: "index-budget", Nonce: "index-budget", RawBytes: 31, UsedBytes: 17, At: time.Now().UTC(), Meta: "immutable evidence"}
	if err := db.Omit("user_id", "subscription_id", "node_id", "protocol_endpoint_id").Create(&fact).Error; err != nil {
		t.Fatal(err)
	}
	var before model.TrafficRecord
	if err := db.First(&before, fact.ID).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := datastore.ReconcileTrafficReadSchema(db); err != nil {
			t.Fatal("upgrade/restart", i, err)
		}
	}
	for _, index := range []struct{ table, name string }{{"traffic_records", "idx_traffic_user"}, {"traffic_records", "idx_traffic_time"}, {"flow_usages", "idx_flow_usages_principal"}, {"flow_usages", "idx_flow_usages_status_seen"}} {
		if db.Migrator().HasIndex(index.table, index.name) {
			t.Fatal("redundant write amplification retained", index)
		}
	}
	for _, index := range []struct{ table, name string }{{"traffic_records", "ux_traffic_node_report"}, {"traffic_records", "ux_traffic_node_nonce"}, {"traffic_records", "idx_traffic_records_user_time"}, {"flow_usages", "ux_flow_usage_node_flow"}, {"flow_usages", "idx_flow_usages_endpoint_active"}} {
		if !db.Migrator().HasIndex(index.table, index.name) {
			t.Fatal("canonical lookup removed", index)
		}
	}
	if datastore.IsSQLite(db) && !db.Migrator().HasIndex("traffic_records", "idx_traffic_records_at") {
		t.Fatal("existing SQLite cursor index removed")
	}
	if !datastore.IsSQLite(db) && !db.Migrator().HasIndex("traffic_records", "idx_traffic_records_history_cursor") {
		t.Fatal("existing MySQL cursor index removed")
	}
	var after model.TrafficRecord
	if err := db.First(&after, fact.ID).Error; err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("index budget changed a financial fact", after, before, err)
	}
}
func TestSQLiteMeteringIndexBudgetRefusesUnknownNamedIndex(t *testing.T) {
	db := diagnosticSchemaDB(t, false)
	if err := db.Exec("CREATE UNIQUE INDEX idx_traffic_user ON traffic_records(user_id)").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.ReconcileTrafficReadSchema(db); err == nil {
		t.Fatal("unknown unique index silently removed")
	}
	if !db.Migrator().HasIndex("traffic_records", "idx_traffic_user") {
		t.Fatal("custom uniqueness removed")
	}
}
