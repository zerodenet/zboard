package datastore_test

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

var zeroEventInlineIndexColumns = map[string]map[string][]string{
	"principal_flow_observations": {
		"idx_principal_flow_observation_user_timeline":         {"user_id", "node_id", "principal_key", "observed_at", "id"},
		"idx_principal_flow_observation_subscription_timeline": {"subscription_id", "node_id", "principal_key", "observed_at", "id"},
	},
	"principal_flow_currents": {
		"idx_principal_flow_current_endpoint_usage": {"protocol_endpoint_id", "active_flows", "user_id", "observed_at"},
	},
	"principal_flow_scope_observations": {
		"idx_principal_flow_scope_boundary_timeline": {"scope_type", "scope_id", "source", "node_id", "observed_at", "id"},
	},
}

var trafficRecordReadIndexColumns = map[string][]string{
	"idx_traffic_records_user_time":               {"user_id", "record_at"},
	"idx_traffic_records_subscription_time":       {"subscription_id", "record_at"},
	"idx_traffic_records_subscription_created_at": {"subscription_id", "created_at"},
	"idx_traffic_records_subscription_usage":      {"subscription_id", "used_bytes"},
	"idx_traffic_records_node_time":               {"node_id", "record_at"},
	"idx_traffic_records_endpoint_time":           {"protocol_endpoint_id", "record_at"},
}

func assertZeroEventInlineIndexColumns(t *testing.T, db *gorm.DB) {
	t.Helper()
	for table, expected := range zeroEventInlineIndexColumns {
		assertReadIndexColumns(t, db, table, expected)
	}
}

func assertReadIndexColumns(t *testing.T, db *gorm.DB, table string, expected map[string][]string) {
	t.Helper()
	indexes, err := db.Migrator().GetIndexes(table)
	if err != nil {
		t.Fatal(err)
	}
	actual := map[string][]string{}
	for _, index := range indexes {
		actual[index.Name()] = index.Columns()
	}
	for name, columns := range expected {
		if !reflect.DeepEqual(actual[name], columns) {
			t.Fatalf("%s.%s columns = %v, want %v", table, name, actual[name], columns)
		}
	}
}

func mysqlSessionAlterCount(t *testing.T, pool *sql.DB) (uint64, uint64) {
	t.Helper()
	var connectionID, count uint64
	var variable string
	if err := pool.QueryRow("SELECT CONNECTION_ID()").Scan(&connectionID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow("SHOW SESSION STATUS LIKE 'Com_alter_table'").Scan(&variable, &count); err != nil {
		t.Fatal(err)
	}
	return connectionID, count
}

func assertTrafficReconcileAlterCount(t *testing.T, db *gorm.DB, want uint64) {
	t.Helper()
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// A single retained connection makes SESSION counters attributable to this
	// reconciliation, and the ID check rejects reconnects that reset counters.
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	pool.SetConnMaxLifetime(0)
	pool.SetConnMaxIdleTime(0)
	beforeID, before := mysqlSessionAlterCount(t, pool)
	if err := datastore.ReconcileTrafficReadSchema(db); err != nil {
		t.Fatal(err)
	}
	afterID, after := mysqlSessionAlterCount(t, pool)
	if beforeID != afterID || after < before || after-before != want {
		t.Fatalf("ALTER count: connection %d→%d, count %d→%d, want delta %d", beforeID, afterID, before, after, want)
	}
	t.Logf("same MySQL session %d: Com_alter_table delta=%d", beforeID, after-before)
}

func zeroEventIndexRows(t *testing.T, db *gorm.DB) map[string][]map[string]any {
	t.Helper()
	result := map[string][]map[string]any{}
	var tables []string
	for table := range zeroEventInlineIndexColumns {
		tables = append(tables, table)
	}
	if db.Migrator().HasTable("traffic_records") {
		tables = append(tables, "traffic_records")
	}
	for _, table := range tables {
		var rows []map[string]any
		if err := db.Table(table).Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		result[table] = rows
	}
	return result
}

func seedZeroEventIndexRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	rows := map[string]map[string]any{
		"principal_flow_observations":       {"id": 1, "node_id": 4, "core_instance_id": "retained-core", "session_registry_revision": 1, "event_id": "retained-observation", "sequence": 1, "principal_key": "retained-principal", "user_id": 2, "subscription_id": 3, "protocol_credential_id": 0, "protocol_endpoint_id": 0, "active_flows": 7, "observed_at": now, "created_at": now},
		"principal_flow_currents":           {"node_id": 4, "principal_key": "retained-principal", "core_instance_id": "retained-core", "session_registry_revision": 1, "user_id": 2, "subscription_id": 3, "protocol_credential_id": 0, "protocol_endpoint_id": 0, "active_flows": 7, "observed_at": now, "updated_at": now},
		"principal_flow_scope_observations": {"id": 1, "scope_type": "subscription", "scope_id": 3, "active_flows": 7, "node_id": 4, "core_instance_id": "retained-core", "session_registry_revision": 1, "event_id": "retained-scope", "source": "lifecycle", "observed_at": now, "created_at": now},
	}
	for table, row := range rows {
		if err := db.Table(table).Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestMySQLZeroEventFreshIndexesAvoidAlterAndRecoverOldTables(t *testing.T) {
	db, root := hourlyMySQLFixture(t)
	// Use only the projection creation boundary: traffic-read reconciliation has
	// not run, so these four indexes must come from CREATE TABLE itself.
	if err := datastore.ReconcileZeroEventSchema(db); err != nil {
		t.Fatal(err)
	}
	assertZeroEventInlineIndexColumns(t, db)
	seedZeroEventIndexRows(t, db)
	wantRows := zeroEventIndexRows(t, db)
	var identity struct {
		Account string
		Schema  string
	}
	if err := db.Raw("SELECT CURRENT_USER() AS account, DATABASE() AS `schema`").Scan(&identity).Error; err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(identity.Account, "@")
	if len(parts) != 2 {
		t.Fatalf("unexpected fixture account %q", identity.Account)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	grantTarget := "`" + strings.ReplaceAll(identity.Schema, "`", "``") + "`.* TO " + quote(parts[0]) + "@" + quote(parts[1])
	if _, err := root.Exec("REVOKE ALTER ON " + strings.Replace(grantTarget, " TO ", " FROM ", 1)); err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// Database privileges can be cached by a connection. Force fresh sessions
	// so a successful reconciliation proves it did not need ALTER permission.
	pool.SetMaxIdleConns(0)
	err = db.Exec("ALTER TABLE principal_flow_currents ADD KEY forbidden_probe (active_flows)").Error
	var denied *mysqldriver.MySQLError
	if !errors.As(err, &denied) || denied.Number != 1142 {
		t.Fatalf("fixture must reject ALTER with 1142: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := datastore.ReconcileTrafficReadSchema(db); err != nil {
			t.Fatalf("fresh indexes unexpectedly need ALTER: %v", err)
		}
	}
	t.Log("fresh projection reconciliation succeeded with ALTER privilege revoked")
	if _, err := root.Exec("GRANT ALTER ON " + grantTarget); err != nil {
		t.Fatal(err)
	}
	pool.SetMaxIdleConns(1)
	assertTrafficReconcileAlterCount(t, db, 0)
	// An old traffic ledger with none of the six additive indexes needs one
	// ALTER, regardless of how many missing keys the statement must install.
	if err := db.Exec(`CREATE TABLE traffic_records (
		id bigint unsigned PRIMARY KEY, user_id bigint unsigned NOT NULL,
		subscription_id bigint unsigned NOT NULL, node_id bigint unsigned NOT NULL,
		protocol_endpoint_id bigint unsigned NOT NULL, record_at datetime(3) NOT NULL,
		created_at datetime(3) NOT NULL, used_bytes bigint NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO traffic_records VALUES (1,2,3,4,5,?,?,17)", time.Now().UTC(), time.Now().UTC()).Error; err != nil {
		t.Fatal(err)
	}
	wantRows = zeroEventIndexRows(t, db)
	assertTrafficReconcileAlterCount(t, db, 1)
	assertReadIndexColumns(t, db, "traffic_records", trafficRecordReadIndexColumns)
	assertTrafficReconcileAlterCount(t, db, 0)
	if got := zeroEventIndexRows(t, db); !reflect.DeepEqual(got, wantRows) {
		t.Fatal("batched ledger index creation changed rows")
	}
	// A failed grouped ALTER remains visible with the original driver error;
	// reconciliation must not pretend a missing index was repaired.
	if err := db.Migrator().DropIndex("traffic_records", "idx_traffic_records_user_time"); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Exec("REVOKE ALTER ON " + strings.Replace(grantTarget, " TO ", " FROM ", 1)); err != nil {
		t.Fatal(err)
	}
	pool.SetMaxIdleConns(0)
	err = datastore.ReconcileTrafficReadSchema(db)
	denied = nil
	if !errors.As(err, &denied) || denied.Number != 1142 {
		t.Fatalf("grouped ALTER must propagate driver error 1142: %v", err)
	}
	if db.Migrator().HasIndex("traffic_records", "idx_traffic_records_user_time") {
		t.Fatal("denied ALTER unexpectedly repaired the missing index")
	}
	if got := zeroEventIndexRows(t, db); !reflect.DeepEqual(got, wantRows) {
		t.Fatal("failed grouped ALTER changed rows")
	}
	if _, err := root.Exec("GRANT ALTER ON " + grantTarget); err != nil {
		t.Fatal(err)
	}
	assertTrafficReconcileAlterCount(t, db, 1)
	// Inject both a completely missing index set and a partially repaired set.
	// Keep all existing-table checks and ALTER recovery on the original path.
	for _, partial := range []bool{false, true} {
		for table, indexes := range zeroEventInlineIndexColumns {
			for name := range indexes {
				if err := db.Migrator().DropIndex(table, name); err != nil {
					t.Fatal(err)
				}
			}
		}
		for name := range trafficRecordReadIndexColumns {
			if err := db.Migrator().DropIndex("traffic_records", name); err != nil {
				t.Fatal(err)
			}
		}
		if partial {
			if err := db.Exec("CREATE INDEX idx_principal_flow_observation_user_timeline ON principal_flow_observations (user_id,node_id,principal_key,observed_at,id)").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("CREATE INDEX idx_traffic_records_user_time ON traffic_records (user_id,record_at)").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("CREATE INDEX idx_traffic_records_node_time ON traffic_records (node_id,record_at)").Error; err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < 2; i++ {
			if err := datastore.ReconcileZeroEventSchema(db); err != nil {
				t.Fatal(err)
			}
			wantAlters := uint64(4)
			if i > 0 {
				wantAlters = 0
			}
			assertTrafficReconcileAlterCount(t, db, wantAlters)
			assertZeroEventInlineIndexColumns(t, db)
			assertReadIndexColumns(t, db, "traffic_records", trafficRecordReadIndexColumns)
			if got := zeroEventIndexRows(t, db); !reflect.DeepEqual(got, wantRows) {
				t.Fatalf("partial=%t pass=%d recovery changed observations", partial, i)
			}
		}
	}
}

func TestSQLiteZeroEventIndexCreationAndRecoveryUnchanged(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "inline-projections.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	if err := datastore.ReconcileTrafficReadSchema(db); err != nil {
		t.Fatal(err)
	}
	assertZeroEventInlineIndexColumns(t, db)
	seedZeroEventIndexRows(t, db)
	wantRows := zeroEventIndexRows(t, db)
	for table, indexes := range zeroEventInlineIndexColumns {
		for name := range indexes {
			if err := db.Exec(fmt.Sprintf("DROP INDEX %s", name)).Error; err != nil {
				t.Fatal(table, name, err)
			}
		}
	}
	for i := 0; i < 2; i++ {
		if err := datastore.ReconcileZeroEventSchema(db); err != nil {
			t.Fatal(err)
		}
		if err := datastore.ReconcileTrafficReadSchema(db); err != nil {
			t.Fatal(err)
		}
		assertZeroEventInlineIndexColumns(t, db)
		if got := zeroEventIndexRows(t, db); !reflect.DeepEqual(got, wantRows) {
			t.Fatal("SQLite index recovery changed observations")
		}
	}
}
