package datastore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Bridge only in test binaries; real integration still enters RunMigrations.
func ReconcileNodeAssetForTest(db *gorm.DB) error { return reconcileNodeAssetSchema(db) }

func TestNodeAssetCopyClassifiesLegacyCurrentAndLostReceipt(t *testing.T) {
	for _, mode := range []string{"legacy", "current", "unknown_current", "receipt_lost_column", "receipt_lost_table"} {
		t.Run(mode, func(t *testing.T) {
			source, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "source.db"))
			if err != nil {
				t.Fatal(err)
			}
			target, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "target.db"))
			if err != nil {
				t.Fatal(err)
			}
			for _, db := range []*gorm.DB{source, target} {
				pool, _ := db.DB()
				t.Cleanup(func() { _ = pool.Close() })
			}
			// Narrow structural copy contract, not a replacement for real prepared DBs.
			if err := source.AutoMigrate(&schemaMigration{}); err != nil {
				t.Fatal(err)
			}
			schema := "CREATE TABLE nodes(id INTEGER PRIMARY KEY,name TEXT,asset_tag TEXT NOT NULL DEFAULT '')"
			if mode == "legacy" || mode == "receipt_lost_column" {
				schema = "CREATE TABLE nodes(id INTEGER PRIMARY KEY,name TEXT)"
			}
			if err := source.Exec(schema).Error; err != nil {
				t.Fatal(err)
			}
			if err := target.Exec("CREATE TABLE nodes(id INTEGER PRIMARY KEY,name TEXT,asset_tag TEXT NOT NULL DEFAULT '')").Error; err != nil {
				t.Fatal(err)
			}
			if mode != "legacy" {
				if err := source.Create(&schemaMigration{Version: nodeAssetSchemaVersion, AppliedAt: time.Now().UTC()}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if mode == "receipt_lost_table" {
				if err := source.Exec("DROP TABLE nodes").Error; err != nil {
					t.Fatal(err)
				}
			} else {
				row := map[string]any{"id": 1, "name": "preserved-canonical"}
				if mode == "current" {
					row["asset_tag"] = "exact-source-label"
				}
				if mode == "unknown_current" {
					row["asset_tag"] = ""
				}
				if err := source.Table("nodes").Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			rollback := errors.New("test copy rollback")
			var copied string
			err = target.Transaction(func(tx *gorm.DB) error {
				if err := CopyApplicationData(source, tx); err != nil {
					return err
				}
				if err := tx.Table("nodes").Select("asset_tag").Where("id = 1").Scan(&copied).Error; err != nil {
					return err
				}
				return rollback
			})
			if strings.HasPrefix(mode, "receipt_lost_") {
				if err == nil || !strings.Contains(err.Error(), "recorded node asset identity is missing") {
					t.Fatal("lost source identity was copied as legacy", err)
				}
				if err := RunMigrations(source); err == nil || !strings.Contains(err.Error(), "recorded node asset identity is missing") {
					t.Fatal("preflight did not reject before AutoMigrate", err)
				}
				return
			}
			if !errors.Is(err, rollback) {
				t.Fatal("copy contract failed before deliberate rollback", err)
			}
			if mode == "legacy" && len(copied) != 64 || mode == "current" && copied != "exact-source-label" || mode == "unknown_current" && copied != "" {
				t.Fatal("copy guessed or replaced asset identity")
			}
			var count int64
			if err := target.Table("nodes").Count(&count).Error; err != nil || count != 0 {
				t.Fatal("target labels escaped copy rollback", err)
			}
			if mode == "legacy" {
				if source.Migrator().HasColumn("nodes", "asset_tag") {
					t.Fatal("legacy copy modified source schema")
				}
			} else {
				var original string
				if err := source.Table("nodes").Select("asset_tag").Where("id = 1").Scan(&original).Error; err != nil || original != copied {
					t.Fatal("copy modified current source label", err)
				}
			}
		})
	}
}

var errNodeAssetMetadata = errors.New("test node asset metadata failure")

type nodeAssetMetaConn struct {
	stage, fault  string
	execs, closes int
}
type nodeAssetMetaConnector struct{ conn *nodeAssetMetaConn }

func (c nodeAssetMetaConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c nodeAssetMetaConnector) Driver() driver.Driver                        { return nodeAssetMetaDriver{c.conn} }

type nodeAssetMetaDriver struct{ conn *nodeAssetMetaConn }

func (d nodeAssetMetaDriver) Open(string) (driver.Conn, error) { return d.conn, nil }
func (*nodeAssetMetaConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*nodeAssetMetaConn) Close() error              { return nil }
func (*nodeAssetMetaConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected begin") }
func (c *nodeAssetMetaConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.execs++
	return nil, errors.New("unexpected mutation")
}
func (c *nodeAssetMetaConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	stage := "column"
	cols := []string{"COLUMN_TYPE", "IS_NULLABLE", "COLUMN_DEFAULT"}
	values := []driver.Value{"varchar(64)", "NO", ""}
	if strings.Contains(q, "sqlite_master") || strings.Contains(q, "INFORMATION_SCHEMA.TABLES") {
		stage = "tables"
		cols = []string{"name"}
		values = []driver.Value{"nodes"}
	} else if strings.HasPrefix(q, "PRAGMA") {
		cols = []string{"cid", "name", "type", "notnull", "dflt_value", "pk"}
		values = []driver.Value{int64(1), "asset_tag", "TEXT", int64(1), "''", int64(0)}
	}
	fault := ""
	if stage == c.stage {
		fault = c.fault
	}
	if fault == "quote_default" {
		if strings.HasPrefix(q, "PRAGMA") {
			values[4] = "''''"
		} else {
			values[2] = "'"
		}
	}
	if fault == "query" {
		return nil, errNodeAssetMetadata
	}
	if fault == "scan" {
		for i := range values {
			values[i] = nil
		}
	}
	return &nodeAssetMetaRows{conn: c, cols: cols, values: values, fault: fault}, nil
}

type nodeAssetMetaRows struct {
	conn   *nodeAssetMetaConn
	cols   []string
	values []driver.Value
	fault  string
	done   bool
}

func (r *nodeAssetMetaRows) Columns() []string { return r.cols }
func (r *nodeAssetMetaRows) Close() error {
	r.conn.closes++
	if r.fault == "close" {
		return errNodeAssetMetadata
	}
	return nil
}
func (r *nodeAssetMetaRows) Next(out []driver.Value) error {
	if !r.done {
		r.done = true
		copy(out, r.values)
		return nil
	}
	if r.fault == "rows" {
		return errNodeAssetMetadata
	}
	return io.EOF
}

type nodeAssetSQLiteName struct{ gorm.Dialector }

func (nodeAssetSQLiteName) Name() string { return DriverSQLite }

func TestNodeAssetMetadataFailuresNeverInitializeOrRecord(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		for _, stage := range []string{"tables", "column"} {
			for _, fault := range []string{"query", "scan", "rows", "close"} {
				t.Run(strings.Join([]string{map[bool]string{true: "sqlite", false: "mysql"}[sqlite], stage, fault}, "/"), func(t *testing.T) {
					conn := &nodeAssetMetaConn{stage: stage, fault: fault}
					pool := sql.OpenDB(nodeAssetMetaConnector{conn})
					pool.SetMaxOpenConns(1)
					t.Cleanup(func() { _ = pool.Close() })
					db, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
					if err != nil {
						t.Fatal(err)
					}
					if sqlite {
						db.Dialector = nodeAssetSQLiteName{db.Dialector}
					}
					err = reconcileNodeAssetSchema(db)
					if err == nil {
						t.Fatal("metadata failure accepted as legacy")
					}
					if fault != "scan" && !errors.Is(err, errNodeAssetMetadata) {
						t.Fatal("metadata cause lost", err)
					}
					if conn.execs != 0 || pool.Stats().InUse != 0 {
						t.Fatal("metadata failure mutated state or retained connection")
					}
				})
			}
		}
	}
}

func TestNodeAssetRejectsQuotedNonemptySchemaDefault(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(map[bool]string{true: "sqlite", false: "mysql"}[sqlite], func(t *testing.T) {
			conn := &nodeAssetMetaConn{stage: "column", fault: "quote_default"}
			pool := sql.OpenDB(nodeAssetMetaConnector{conn})
			pool.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = pool.Close() })
			db, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
			if err != nil {
				t.Fatal(err)
			}
			if sqlite {
				db.Dialector = nodeAssetSQLiteName{db.Dialector}
			}
			if err := reconcileNodeAssetSchema(db); err == nil || !strings.Contains(err.Error(), "column shape differs") {
				t.Fatal("nonempty quote default accepted as empty", err)
			}
			if conn.execs != 0 || pool.Stats().InUse != 0 {
				t.Fatal("bad default mutated authority or leaked metadata connection")
			}
		})
	}
}

func TestSQLiteNodeAssetDefaultQuoteIsRealNonemptyValue(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "quote-default.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.Exec("CREATE TABLE nodes (id INTEGER PRIMARY KEY,asset_tag TEXT NOT NULL DEFAULT '''')").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO nodes(id) VALUES(1)").Error; err != nil {
		t.Fatal(err)
	}
	var actual string
	if err := db.Table("nodes").Select("asset_tag").Scan(&actual).Error; err != nil || actual != "'" {
		t.Fatal("fixture is not a nonempty quote default", err)
	}
	if err := RunMigrations(db); err == nil || !strings.Contains(err.Error(), "column shape differs") {
		t.Fatal("unsafe default was not refused before AutoMigrate", err)
	}
	if db.Migrator().HasTable("users") {
		t.Fatal("preflight entered automatic schema creation")
	}
}
