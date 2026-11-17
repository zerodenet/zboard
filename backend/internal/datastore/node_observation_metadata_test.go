package datastore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type observationMetaConn struct {
	*nodeAssetMetaConn
	sqlite bool
}

func (c *observationMetaConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	cols := []string{"COLUMN_NAME"}
	values := []driver.Value{"id"}
	if strings.HasPrefix(q, "PRAGMA") {
		cols = []string{"cid", "name", "type", "notnull", "dflt_value", "pk"}
		values = []driver.Value{int64(0), "id", "TEXT", int64(1), nil, int64(1)}
	}
	if c.fault == "query" {
		return nil, errNodeAssetMetadata
	}
	if c.fault == "scan" {
		for i := range values {
			values[i] = nil
		}
	}
	return &nodeAssetMetaRows{conn: c.nodeAssetMetaConn, cols: cols, values: values, fault: c.fault}, nil
}

type observationMetaConnector struct{ conn *observationMetaConn }

func (c observationMetaConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c observationMetaConnector) Driver() driver.Driver {
	return nodeAssetMetaDriver{c.conn.nodeAssetMetaConn}
}
func TestObservationMetadataErrorsNeverBecomeAbsence(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		for _, fault := range []string{"query", "scan", "rows", "close"} {
			t.Run(strings.Join([]string{map[bool]string{true: "sqlite", false: "mysql"}[sqlite], fault}, "/"), func(t *testing.T) {
				conn := &observationMetaConn{nodeAssetMetaConn: &nodeAssetMetaConn{fault: fault}, sqlite: sqlite}
				pool := sql.OpenDB(observationMetaConnector{conn})
				pool.SetMaxOpenConns(1)
				t.Cleanup(func() { _ = pool.Close() })
				db, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
				if err != nil {
					t.Fatal(err)
				}
				if sqlite {
					db.Dialector = nodeAssetSQLiteName{db.Dialector}
				}
				_, err = observationColumnNames(db, "node_observation_intents")
				if err == nil {
					t.Fatal("metadata failure accepted as missing columns")
				}
				if fault != "scan" && !errors.Is(err, errNodeAssetMetadata) {
					t.Fatal("cause lost", err)
				}
				if conn.execs != 0 || pool.Stats().InUse != 0 {
					t.Fatal("metadata failure mutated or retained connection")
				}
			})
		}
	}
}
