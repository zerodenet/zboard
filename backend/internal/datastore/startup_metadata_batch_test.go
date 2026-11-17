package datastore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This is a pure MySQL-dialect driver contract, NOT a real MySQL integration
// result. Every query is classified/countable; unexpected SQL fails. Existing
// real SQLite/MySQL migration/authority/index/copy suites remain mandatory.
var errStartupMetadata = errors.New("injected startup metadata failure")

type startupMetadataColumn struct{ kind, nullable, collation string }
type startupMetadataConn struct {
	tables               map[string]bool
	columns              map[string]map[string]startupMetadataColumn
	primary              map[string]string
	queries, execs       []string
	openRows             int
	faultKind, faultMode string
	selected             string
	applied              int64
}
type startupMetadataConnector struct{ conn *startupMetadataConn }

func (c startupMetadataConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c startupMetadataConnector) Driver() driver.Driver                        { return startupMetadataDriver{c.conn} }

type startupMetadataDriver struct{ conn *startupMetadataConn }

func (d startupMetadataDriver) Open(string) (driver.Conn, error) { return d.conn, nil }
func (c *startupMetadataConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected metadata Prepare")
}
func (c *startupMetadataConn) Close() error              { return nil }
func (c *startupMetadataConn) Begin() (driver.Tx, error) { return startupMetadataTx{}, nil }
func (c *startupMetadataConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return startupMetadataTx{}, nil
}

type startupMetadataTx struct{}

func (startupMetadataTx) Commit() error   { return nil }
func (startupMetadataTx) Rollback() error { return nil }
func (c *startupMetadataConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if c.openRows != 0 {
		return nil, errors.New("write attempted while metadata rows remain open")
	}
	c.execs = append(c.execs, q)
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(q)), "insert into `schema_migrations`") {
		return nil, errors.New("unexpected DDL/business write in prepared-schema fast path")
	}
	return driver.RowsAffected(1), nil
}
func normalizeStartupSQL(q string) string {
	return strings.ToLower(strings.Join(strings.Fields(q), " "))
}
func (c *startupMetadataConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if c.openRows != 0 {
		return nil, errors.New("follow-up query attempted before metadata Close")
	}
	c.queries = append(c.queries, q)
	sql := normalizeStartupSQL(q)
	arg := func(i int) string {
		if i >= len(args) {
			return ""
		}
		v, _ := args[i].Value.(string)
		return v
	}
	kind := ""
	var names []string
	var values [][]driver.Value
	scalar := func(name string, v driver.Value) { names = []string{name}; values = [][]driver.Value{{v}} }
	switch {
	case strings.HasPrefix(sql, "select database(),t.table_name from"):
		kind = "inventory"
		names = []string{"DATABASE()", "TABLE_NAME"}
		for _, table := range []string{"schema_migrations", "orders", "order_discount_redemptions"} {
			if c.tables[table] {
				values = append(values, []driver.Value{c.selected, table})
			}
		}
		if len(values) == 0 {
			values = [][]driver.Value{{c.selected, nil}}
		}
	case sql == "select database()":
		scalar("DATABASE()", c.selected)
	case strings.HasPrefix(sql, "select schema_name from information_schema.schemata"):
		scalar("SCHEMA_NAME", c.selected)
	case strings.HasPrefix(sql, "select table_name from information_schema.tables"):
		names = []string{"TABLE_NAME"}
		var tables []string
		for table, present := range c.tables {
			if present {
				tables = append(tables, table)
			}
		}
		sort.Strings(tables)
		for _, table := range tables {
			values = append(values, []driver.Value{table})
		}
	case strings.HasPrefix(sql, "select count(*) from information_schema.tables"):
		n := int64(0)
		if c.tables[arg(1)] {
			n = 1
		}
		scalar("count(*)", n)
	case strings.HasPrefix(sql, "select count(*) from information_schema.columns"):
		n := int64(0)
		if _, ok := c.columns[arg(1)][strings.ToLower(arg(2))]; ok {
			n = 1
		}
		scalar("count(*)", n)
	case strings.Contains(sql, "select count(*) from `schema_migrations`"):
		scalar("count(*)", c.applied)
	case strings.HasPrefix(sql, "select engine,table_type from information_schema.tables"):
		names = []string{"ENGINE", "TABLE_TYPE"}
		values = [][]driver.Value{{"InnoDB", "BASE TABLE"}}
	case strings.HasPrefix(sql, "select column_name,column_type,is_nullable,collation_name,column_default from information_schema.columns"):
		names = []string{"COLUMN_NAME", "COLUMN_TYPE", "IS_NULLABLE", "COLLATION_NAME", "COLUMN_DEFAULT"}
		var columns []string
		for column := range c.columns[arg(0)] {
			columns = append(columns, column)
		}
		sort.Strings(columns)
		for _, column := range columns {
			v := c.columns[arg(0)][column]
			values = append(values, []driver.Value{column, v.kind, v.nullable, v.collation, nil})
		}
	case strings.HasPrefix(sql, "select column_name from information_schema.columns"):
		kind = "columns"
		names = []string{"COLUMN_NAME"}
		var columns []string
		for column := range c.columns[arg(0)] {
			columns = append(columns, column)
		}
		sort.Strings(columns)
		for _, column := range columns {
			values = append(values, []driver.Value{column})
		}
	case strings.HasPrefix(sql, "select seq_in_index,column_name,sub_part from information_schema.statistics"):
		names = []string{"SEQ_IN_INDEX", "COLUMN_NAME", "SUB_PART"}
		if pk := c.primary[arg(0)]; pk != "" {
			values = [][]driver.Value{{int64(1), pk, nil}}
		}
	case strings.HasPrefix(sql, "select table_name, column_name, index_name, non_unique from information_schema.statistics"):
		names = []string{"TABLE_NAME", "COLUMN_NAME", "INDEX_NAME", "NON_UNIQUE"}
		table := arg(1)
		specs := []struct {
			name, columns string
			nonunique     int64
		}{}
		if table == "order_discount_redemptions" {
			specs = []struct {
				name, columns string
				nonunique     int64
			}{{"idx_order_discount_redemption_key", "redemption_key", 0}, {"idx_order_discount_redemption_due", "next_attempt_at,order_id", 1}, {"idx_order_discount_redemption_expiry", "desired,created_at,order_id", 1}}
		}
		if table == "subscription_automatic_renewals" {
			specs = []struct {
				name, columns string
				nonunique     int64
			}{{"idx_auto_pending_order", "pending_order_id", 0}, {"idx_auto_pending_due", "pending_order_id,next_attempt_at,user_id", 1}}
		}
		for _, spec := range specs {
			for _, column := range strings.Split(spec.columns, ",") {
				values = append(values, []driver.Value{table, column, spec.name, spec.nonunique})
			}
		}
	case strings.HasPrefix(sql, "select id, octet_length(purchase_snapshot) as snapshot_bytes from `orders`"):
		names = []string{"id", "snapshot_bytes"} // empty legacy source, not a fabricated hold.
	default:
		return nil, fmt.Errorf("unexpected metadata SQL category: %s", sql)
	}
	if kind == c.faultKind && c.faultMode == "query" {
		return nil, errStartupMetadata
	}
	c.openRows++
	return &startupMetadataRows{conn: c, names: names, values: values, fault: kind != "" && kind == c.faultKind}, nil
}

type startupMetadataRows struct {
	conn          *startupMetadataConn
	names         []string
	values        [][]driver.Value
	next          int
	closed, fault bool
}

func (r *startupMetadataRows) Columns() []string { return r.names }
func (r *startupMetadataRows) Close() error {
	if !r.closed {
		r.closed = true
		r.conn.openRows--
	}
	if r.fault && r.conn.faultMode == "close" {
		return errStartupMetadata
	}
	return nil
}
func (r *startupMetadataRows) Next(out []driver.Value) error {
	if r.fault && r.conn.faultMode == "scan" && r.next == 0 {
		r.next++
		for i := range out {
			out[i] = struct{}{}
		}
		return nil
	}
	if r.next < len(r.values) {
		copy(out, r.values[r.next])
		r.next++
		return nil
	}
	if r.fault && r.conn.faultMode == "rows" {
		return errStartupMetadata
	}
	return io.EOF
}
func preparedStartupMetadata(t *testing.T) (*gorm.DB, *startupMetadataConn, *sql.DB) {
	t.Helper()
	c := &startupMetadataConn{selected: "startup_metadata_fixture", applied: 1, tables: map[string]bool{"schema_migrations": true, "orders": true, "order_discount_redemptions": true, "subscription_automatic_renewals": true, "audit_logs": true}, columns: map[string]map[string]startupMetadataColumn{}, primary: map[string]string{"order_discount_redemptions": "order_id", "subscription_automatic_renewals": "subscription_id"}}
	c.columns["orders"] = map[string]startupMetadataColumn{"purchase_snapshot": {"text", "NO", "utf8mb4_0900_ai_ci"}}
	c.columns["subscription_automatic_renewals"] = map[string]startupMetadataColumn{"subscription_id": {"bigint unsigned", "NO", ""}, "pending_order_id": {"bigint unsigned", "YES", ""}, "pending_consent_revision": {"bigint unsigned", "NO", ""}, "pending_end_at": {"datetime(3)", "YES", ""}, "pending_epoch": {"bigint unsigned", "NO", ""}, "pending_attempt": {"bigint unsigned", "NO", ""}}
	c.columns["order_discount_redemptions"] = map[string]startupMetadataColumn{}
	for _, column := range []string{"order_id", "redemption_key", "binding_hash", "binding_json", "provider_id", "desired", "observed", "token", "revision", "lease_token", "lease_until", "next_attempt_at", "last_error", "created_at", "updated_at"} {
		kind, collation := "varchar(16)", "utf8mb4_0900_ai_ci"
		switch column {
		case "order_id", "revision":
			kind = "bigint unsigned"
			collation = ""
		case "redemption_key", "binding_hash":
			kind = "varchar(64)"
			collation = "ascii_bin"
		case "lease_token", "last_error":
			kind = "varchar(64)"
		case "binding_json":
			kind = "text"
		case "provider_id":
			kind = "varchar(160)"
		case "token":
			kind = "varchar(512)"
		case "lease_until", "next_attempt_at", "created_at", "updated_at":
			kind = "datetime(3)"
			collation = ""
		}
		c.columns["order_discount_redemptions"][column] = startupMetadataColumn{kind, "NO", collation}
	}
	pool := sql.OpenDB(startupMetadataConnector{c})
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return db, c, pool
}

func TestStartupMetadataPreparedPathQueryCounts(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		run                    func(*gorm.DB) error
		queries, receiptWrites int
	}{
		{"discount_preflight", preflightDiscountRedemptionSchema, 5, 0},
		{"discount_reconcile", reconcileDiscountRedemptionSchema, 16, 1},
		{"automatic_preflight", preflightAutomaticPendingOrders, 11, 0},
		{"automatic_reconcile", reconcileAutomaticPendingOrders, 29, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, c, pool := preparedStartupMetadata(t)
			if err := tc.run(db); err != nil {
				t.Fatal(err)
			}
			if len(c.queries) != tc.queries {
				t.Fatalf("prepared metadata query count got=%d want=%d", len(c.queries), tc.queries)
			}
			if len(c.execs) != tc.receiptWrites {
				t.Fatal("unexpected DDL/business mutation", len(c.execs))
			}
			if c.openRows != 0 || pool.Stats().InUse != 0 {
				t.Fatal("metadata connection retained")
			}
		})
	}
}
func TestStartupMetadataFreshInspectionAndMissingAuthorityRemainFailClosed(t *testing.T) {
	t.Run("fresh_empty_namespace", func(t *testing.T) {
		db, c, _ := preparedStartupMetadata(t)
		c.tables = map[string]bool{}
		if err := preflightDiscountRedemptionSchema(db); err != nil {
			t.Fatal(err)
		}
		if len(c.queries) != 1 || len(c.execs) != 0 {
			t.Fatal("fresh preflight was not one read")
		}
	})
	t.Run("discount_actual_loss_after_previous_valid_call", func(t *testing.T) {
		db, c, _ := preparedStartupMetadata(t)
		if err := preflightDiscountRedemptionSchema(db); err != nil {
			t.Fatal(err)
		}
		c.tables["order_discount_redemptions"] = false
		if err := preflightDiscountRedemptionSchema(db); err == nil || !strings.Contains(err.Error(), "authority missing") {
			t.Fatal("stale table snapshot hid authority loss", err)
		}
		if len(c.execs) != 0 {
			t.Fatal("refusal repaired authority")
		}
	})
	t.Run("automatic_actual_loss_after_previous_valid_call", func(t *testing.T) {
		db, c, _ := preparedStartupMetadata(t)
		if err := preflightAutomaticPendingOrders(db); err != nil {
			t.Fatal(err)
		}
		delete(c.columns["subscription_automatic_renewals"], "pending_epoch")
		if err := preflightAutomaticPendingOrders(db); err == nil || !strings.Contains(err.Error(), "authority column missing") {
			t.Fatal("stale column snapshot hid authority loss", err)
		}
		if len(c.execs) != 0 {
			t.Fatal("refusal filled unknown epoch")
		}
	})
	for _, damage := range []string{"type", "nullable", "collation", "primary", "extra"} {
		t.Run("discount_"+damage, func(t *testing.T) {
			db, c, _ := preparedStartupMetadata(t)
			column := c.columns["order_discount_redemptions"]["redemption_key"]
			switch damage {
			case "type":
				column.kind = "varchar(32)"
			case "nullable":
				column.nullable = "YES"
			case "collation":
				column.collation = "ascii_general_ci"
			case "primary":
				c.primary["order_discount_redemptions"] = "revision"
			case "extra":
				c.columns["order_discount_redemptions"]["unknown_column"] = column
			}
			c.columns["order_discount_redemptions"]["redemption_key"] = column
			if err := preflightDiscountRedemptionSchema(db); err == nil {
				t.Fatal("physical contract damage accepted")
			}
			if len(c.execs) != 0 {
				t.Fatal("shape refusal wrote")
			}
		})
	}
}
func TestStartupMetadataUnrecordedEmptyLegacyOrdersRetainSemanticProbe(t *testing.T) {
	db, c, pool := preparedStartupMetadata(t)
	c.applied = 0
	c.tables["order_discount_redemptions"] = false
	if err := preflightDiscountRedemptionSchema(db); err != nil {
		t.Fatal(err)
	}
	if len(c.queries) != 4 {
		t.Fatalf("unrecorded legacy metadata/probe query count got=%d want=4", len(c.queries))
	}
	if len(c.execs) != 0 || c.openRows != 0 || pool.Stats().InUse != 0 {
		t.Fatal("legacy inspection mutated source or retained rows")
	}
}

func TestStartupMetadataBatchErrorsPropagateBeforeAnyWrite(t *testing.T) {
	for _, kind := range []string{"inventory", "columns"} {
		for _, mode := range []string{"query", "scan", "rows", "close"} {
			t.Run(kind+"_"+mode, func(t *testing.T) {
				db, c, pool := preparedStartupMetadata(t)
				c.faultKind, c.faultMode = kind, mode
				run := reconcileDiscountRedemptionSchema
				if kind == "columns" {
					run = reconcileAutomaticPendingOrders
				}
				err := run(db)
				if err == nil {
					t.Fatal("metadata failure treated as missing/complete schema")
				}
				if mode != "scan" && !errors.Is(err, errStartupMetadata) {
					t.Fatal("metadata error identity lost", err)
				}
				if len(c.execs) != 0 || c.openRows != 0 || pool.Stats().InUse != 0 {
					t.Fatal("metadata error wrote/retained resources")
				}
			})
		}
	}
}
