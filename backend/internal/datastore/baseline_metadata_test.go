package datastore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
)

// External integration tests still prepare real schemas through the public
// boundary. This bridge measures/checks the private validator in isolation.
func ValidatePreReleaseBaselineSchemaForTest(db *sql.DB) error {
	return validatePreReleaseBaselineSchema(db)
}

var errBaselineMetadataRead = errors.New("baseline metadata read failed")

type baselineMetadataConnector struct{ conn *baselineMetadataConn }

func (c baselineMetadataConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c baselineMetadataConnector) Driver() driver.Driver                        { return baselineMetadataDriver{c.conn} }

type baselineMetadataDriver struct{ conn *baselineMetadataConn }

func (d baselineMetadataDriver) Open(string) (driver.Conn, error) { return d.conn, nil }

type baselineMetadataConn struct {
	faultStage, faultMode string
	missingColumn         string
	missingIndex          string
	wrongType             bool
	missingTable          bool
	stages                []string
}

func (*baselineMetadataConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*baselineMetadataConn) Close() error { return nil }
func (*baselineMetadataConn) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}
func (c *baselineMetadataConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	query = strings.ToLower(query)
	rows := &baselineMetadataRows{}
	stage := "tables"
	switch {
	case strings.Contains(query, "information_schema.tables"):
		rows.names = []string{"count"}
		count := len(preReleaseBaselineTables)
		if c.missingTable {
			count--
		}
		rows.values = [][]driver.Value{{int64(count)}}
	case strings.Contains(query, "information_schema.columns"):
		stage = "columns"
		rows.names = []string{"table_name", "column_name", "column_type"}
		legacy := strings.HasPrefix(query, "select column_type from")
		if legacy {
			rows.names = []string{"column_type"}
		}
		for i := len(preReleaseBaselineColumns) - 1; i >= 0; i-- {
			spec := preReleaseBaselineColumns[i]
			if spec.table+"."+spec.column == c.missingColumn || (legacy && (spec.table != args[0].Value || spec.column != args[1].Value)) {
				continue
			}
			typeName := spec.columnType
			if c.wrongType && i == 0 {
				typeName = "char(128)"
			}
			if legacy {
				rows.values = append(rows.values, []driver.Value{typeName})
			} else {
				// Metadata result order and identifier casing must not replace
				// the original definition/check order.
				rows.values = append(rows.values, []driver.Value{strings.ToUpper(spec.table), strings.ToUpper(spec.column), typeName})
			}
		}
	case strings.Contains(query, "information_schema.statistics"):
		stage = "indexes"
		rows.names = []string{"table_name", "index_name"}
		legacy := strings.HasPrefix(query, "select count(*)")
		if legacy {
			rows.names = []string{"count"}
		}
		for i := len(preReleaseBaselineIndexes) - 1; i >= 0; i-- {
			spec := preReleaseBaselineIndexes[i]
			if legacy {
				if spec.table == args[0].Value && spec.index == args[1].Value {
					count := int64(1)
					if spec.table+"."+spec.index == c.missingIndex {
						count = 0
					}
					rows.values = append(rows.values, []driver.Value{count})
				}
			} else if spec.table+"."+spec.index != c.missingIndex {
				rows.values = append(rows.values, []driver.Value{strings.ToUpper(spec.table), strings.ToUpper(spec.index)})
			}
		}
	default:
		return nil, errors.New("unexpected metadata query")
	}
	c.stages = append(c.stages, stage)
	if c.faultStage == stage {
		switch c.faultMode {
		case "query":
			return nil, errBaselineMetadataRead
		case "scan":
			rows.values[0][0] = nil
		case "rows":
			rows.values = rows.values[:1]
			rows.terminal = errBaselineMetadataRead
		}
	}
	return rows, nil
}

type baselineMetadataRows struct {
	names    []string
	values   [][]driver.Value
	next     int
	terminal error
}

func (r *baselineMetadataRows) Columns() []string { return r.names }
func (*baselineMetadataRows) Close() error        { return nil }
func (r *baselineMetadataRows) Next(out []driver.Value) error {
	if r.next < len(r.values) {
		copy(out, r.values[r.next])
		r.next++
		return nil
	}
	if r.terminal != nil {
		return r.terminal
	}
	return io.EOF
}

func baselineMetadataTestDB(t *testing.T, conn *baselineMetadataConn) *sql.DB {
	t.Helper()
	db := sql.OpenDB(baselineMetadataConnector{conn})
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestBaselineMetadataBatchesReadsAndPreservesValidationOrder(t *testing.T) {
	for _, test := range []struct {
		name, want string
		conn       baselineMetadataConn
		stages     string
	}{
		{name: "valid", stages: "tables,columns,indexes"},
		{name: "table_first", conn: baselineMetadataConn{missingTable: true, wrongType: true}, want: "schema is incomplete", stages: "tables"},
		{name: "type_before_later_missing", conn: baselineMetadataConn{wrongType: true, missingColumn: "email_templates.subject_template", faultStage: "indexes", faultMode: "query"}, want: "flow_usages.flow_id has type char(128), want varchar(128)", stages: "tables,columns"},
		{name: "missing_before_indexes", conn: baselineMetadataConn{missingColumn: "flow_usages.flow_id", faultStage: "indexes", faultMode: "query"}, want: "missing column flow_usages.flow_id", stages: "tables,columns"},
		{name: "missing_index", conn: baselineMetadataConn{missingIndex: "audit_logs.idx_audit_logs_history_cursor"}, want: "missing index audit_logs.idx_audit_logs_history_cursor", stages: "tables,columns,indexes"},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := test.conn
			err := validatePreReleaseBaselineSchema(baselineMetadataTestDB(t, &conn))
			if (test.want == "" && err != nil) || (test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want))) {
				t.Fatalf("validation result changed: %v", err)
			}
			if strings.Join(conn.stages, ",") != test.stages {
				t.Fatalf("metadata phases/order changed: %v want %s", conn.stages, test.stages)
			}
		})
	}
}

func TestBaselineMetadataPropagatesQueryScanAndRowsErrors(t *testing.T) {
	for _, stage := range []string{"columns", "indexes"} {
		for _, mode := range []string{"query", "scan", "rows"} {
			t.Run(stage+"_"+mode, func(t *testing.T) {
				conn := &baselineMetadataConn{faultStage: stage, faultMode: mode}
				db := baselineMetadataTestDB(t, conn)
				err := validatePreReleaseBaselineSchema(db)
				if err == nil || !strings.Contains(err.Error(), "inspect pre-release baseline "+stage) {
					t.Fatalf("metadata failure lost phase: %v", err)
				}
				if mode == "scan" {
					if !strings.Contains(err.Error(), "converting NULL") {
						t.Fatalf("Scan error lost: %v", err)
					}
				} else if !errors.Is(err, errBaselineMetadataRead) {
					t.Fatalf("read error lost: %v", err)
				}
				if db.Stats().InUse != 0 {
					t.Fatal("failed metadata read retained its only connection")
				}
			})
		}
	}
}
