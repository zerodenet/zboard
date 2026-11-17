package datastore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// These bridges exist only in the test binary. Integration tests still use
// RunMigrations for fresh schemas and the unrecorded 0045 recovery boundary.
func ReconcilePluginOperationColumnsForTest(db *gorm.DB) error {
	return reconcilePluginOperationSchema(db)
}

func ReconcilePublicationPhaseColumnsForTest(db *gorm.DB) error {
	return reconcilePublicationConnectionSchema(db)
}

var errModelColumnMetadata = errors.New("model column metadata unavailable")
var errModelColumnAlter = errors.New("model column ALTER denied")

type modelColumnConnector struct{ conn *modelColumnConn }

func (c modelColumnConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c modelColumnConnector) Driver() driver.Driver                        { return modelColumnDriver{c.conn} }

type modelColumnDriver struct{ conn *modelColumnConn }

func (d modelColumnDriver) Open(string) (driver.Conn, error) { return d.conn, nil }

type modelColumnConn struct {
	columns        []driver.Value
	fault          string
	queries, execs []string
	queryArgs      [][]driver.NamedValue
	execArgs       [][]driver.NamedValue
	closeCalls     int
}

func (*modelColumnConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*modelColumnConn) Close() error { return nil }
func (*modelColumnConn) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}
func (c *modelColumnConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.queries = append(c.queries, query)
	c.queryArgs = append(c.queryArgs, append([]driver.NamedValue(nil), args...))
	if query != "SELECT COLUMN_NAME FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?" {
		return nil, errors.New("unexpected metadata SQL or target-table SELECT")
	}
	if c.fault == "query" {
		return nil, errModelColumnMetadata
	}
	return &modelColumnRows{conn: c}, nil
}
func (c *modelColumnConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.execs = append(c.execs, query)
	c.execArgs = append(c.execArgs, append([]driver.NamedValue(nil), args...))
	if c.fault == "alter" {
		return nil, errModelColumnAlter
	}
	return driver.RowsAffected(0), nil
}

type modelColumnRows struct {
	conn *modelColumnConn
	next int
}

func (*modelColumnRows) Columns() []string { return []string{"COLUMN_NAME"} }
func (r *modelColumnRows) Close() error {
	r.conn.closeCalls++
	if r.conn.fault == "close" {
		return errModelColumnMetadata
	}
	return nil
}
func (r *modelColumnRows) Next(out []driver.Value) error {
	if r.conn.fault == "scan" && r.next == 0 {
		r.next++
		out[0] = nil
		return nil
	}
	if r.next < len(r.conn.columns) {
		out[0] = r.conn.columns[r.next]
		r.next++
		return nil
	}
	if r.conn.fault == "rows" {
		return errModelColumnMetadata
	}
	return io.EOF
}

func modelColumnTestDB(t *testing.T, conn *modelColumnConn) (*gorm.DB, *sql.DB) {
	t.Helper()
	pool := sql.OpenDB(modelColumnConnector{conn})
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return db, pool
}

func TestMySQLModelColumnBatchReadsOnceAndAddsOnlyMissing(t *testing.T) {
	for _, test := range []struct {
		name    string
		columns []driver.Value
		adds    int
	}{
		{name: "all_missing", adds: 4},
		{name: "committed_prefix", columns: []driver.Value{"PREVIOUS_PUBLISHER", "pUbLiShEr"}, adds: 2},
		{name: "complete", columns: []driver.Value{"PREVIOUS_PUBLISHER", "PUBLISHER", "SIGNING_KEY_FINGERPRINT", "PACKAGE_DIGEST"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := &modelColumnConn{columns: test.columns}
			db, pool := modelColumnTestDB(t, conn)
			err := reconcilePluginOperationSchema(db)
			if err != nil {
				t.Fatal(err)
			}
			wantExecs := 0
			if test.adds > 0 {
				wantExecs = 1
			}
			if len(conn.queries) != 1 || len(conn.execs) != wantExecs || strings.Count(strings.Join(conn.execs, ""), "ADD ") != test.adds {
				t.Fatalf("queries=%v, DDL=%v; want one metadata read and %d ALTER(s)/%d additions", conn.queries, conn.execs, wantExecs, test.adds)
			}
			if len(conn.queryArgs[0]) != 1 || conn.queryArgs[0][0].Value != "plugin_operations" || conn.closeCalls != 1 || pool.Stats().InUse != 0 {
				t.Fatal("metadata table scope, close-once, or connection release changed")
			}
		})
	}
}

func TestMySQLModelColumnBatchMetadataErrorsNeverIssueDDL(t *testing.T) {
	for _, mode := range []string{"query", "scan", "rows", "close"} {
		t.Run(mode, func(t *testing.T) {
			conn := &modelColumnConn{fault: mode, columns: []driver.Value{"previous_publisher"}}
			db, pool := modelColumnTestDB(t, conn)
			err := reconcilePluginOperationSchema(db)
			if err == nil || !strings.Contains(err.Error(), "inspect plugin_operations columns") {
				t.Fatalf("metadata failure lost context: %v", err)
			}
			if mode == "scan" {
				if !strings.Contains(err.Error(), "converting NULL") {
					t.Fatalf("Scan error lost: %v", err)
				}
			} else if !errors.Is(err, errModelColumnMetadata) {
				t.Fatalf("driver error lost: %v", err)
			}
			if len(conn.execs) != 0 || pool.Stats().InUse != 0 || (mode != "query" && conn.closeCalls != 1) {
				t.Fatal("metadata failure issued DDL or retained its only connection", conn.execs)
			}
		})
	}
}

type modelColumnExprDialector struct{ gorm.Dialector }
type modelColumnExprMigrator struct{ gorm.Migrator }

func (d modelColumnExprDialector) Migrator(db *gorm.DB) gorm.Migrator {
	return modelColumnExprMigrator{d.Dialector.Migrator(db)}
}
func (m modelColumnExprMigrator) FullDataTypeOf(field *schema.Field) clause.Expr {
	if field.DBName == "publisher" {
		return clause.Expr{SQL: "varchar(160) NOT NULL DEFAULT ?", Vars: []any{"bound-default"}}
	}
	return m.Migrator.FullDataTypeOf(field)
}

func TestMySQLModelColumnBatchPreservesDataTypeVarsAndQuotedNaming(t *testing.T) {
	conn := &modelColumnConn{}
	db, pool := modelColumnTestDB(t, conn)
	db.Dialector = modelColumnExprDialector{db.Dialector}
	db.NamingStrategy = schema.NamingStrategy{TablePrefix: "audit`prefix_"}
	if err := addMissingMySQLModelColumns(db, &model.PluginOperation{}, []string{"Publisher"}); err != nil {
		t.Fatal(err)
	}
	want := "ALTER TABLE `audit``prefix_plugin_operations` ADD `publisher` varchar(160) NOT NULL DEFAULT ?"
	if len(conn.execs) != 1 || conn.execs[0] != want || conn.queryArgs[0][0].Value != "audit`prefix_plugin_operations" {
		t.Fatal("model naming/identifier quoting changed", conn.execs, conn.queryArgs)
	}
	if !reflect.DeepEqual(conn.execArgs[0], []driver.NamedValue{{Ordinal: 1, Value: "bound-default"}}) || pool.Stats().InUse != 0 {
		t.Fatal("FullDataTypeOf Vars lost or metadata connection leaked", conn.execArgs)
	}
}

func TestMySQLModelColumnBatchPropagatesAlterFailure(t *testing.T) {
	conn := &modelColumnConn{fault: "alter"}
	db, pool := modelColumnTestDB(t, conn)
	if err := reconcilePluginOperationSchema(db); !errors.Is(err, errModelColumnAlter) {
		t.Fatal("ALTER failure lost", err)
	}
	if len(conn.execs) != 1 || pool.Stats().InUse != 0 {
		t.Fatal("failed ALTER retried or retained its connection")
	}
}

func TestMySQLModelColumnBatchRejectsUnsupportedFieldsBeforeSQL(t *testing.T) {
	type keyAddition struct {
		ID      uint `gorm:"primaryKey"`
		Counter uint `gorm:"autoIncrement"`
	}
	for _, field := range []string{"ID", "Counter", "Absent"} {
		t.Run(field, func(t *testing.T) {
			conn := &modelColumnConn{}
			db, _ := modelColumnTestDB(t, conn)
			if err := addMissingMySQLModelColumns(db, &keyAddition{}, []string{field}); err == nil {
				t.Fatal("unsupported field silently accepted")
			}
			if len(conn.queries) != 0 || len(conn.execs) != 0 {
				t.Fatal("unsupported field reached SQL")
			}
		})
	}
}
