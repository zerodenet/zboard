package datastore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	mysqlgorm "gorm.io/driver/mysql"
)

func TestOpenContextCancelsMySQLGreetingAndClosesSocket(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted, closed := make(chan struct{}), make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		close(accepted)
		var b [1]byte
		conn.Read(b[:])
		close(closed)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		db, err := OpenWithDriverContext(ctx, DriverMySQL, "test:test@tcp("+listener.Addr().String()+")/test?readTimeout=2s")
		if db != nil {
			pool, _ := db.DB()
			pool.Close()
		}
		result <- err
	}()
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("connection not attempted")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("greeting did not honor cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled greeting remained blocked")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("canceled greeting leaked socket")
	}
}

type versionConnector struct{ connection *versionConnection }

func (c versionConnector) Connect(context.Context) (driver.Conn, error) { return c.connection, nil }
func (c versionConnector) Driver() driver.Driver                        { return versionDriver{c.connection} }

type versionDriver struct{ connection *versionConnection }

func (d versionDriver) Open(string) (driver.Conn, error) { return d.connection, nil }

type versionConnection struct {
	entered chan struct{}
	block   bool
	closed  atomic.Bool
}

func (*versionConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *versionConnection) Close() error            { c.closed.Store(true); return nil }
func (*versionConnection) Begin() (driver.Tx, error) { return nil, errors.New("unexpected begin") }
func (c *versionConnection) QueryContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	if c.block {
		close(c.entered)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
			return nil, errors.New("dialect probe ignored cancellation")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &versionRows{}, nil
}

type versionRows struct{ done bool }

func (*versionRows) Columns() []string { return []string{"version"} }
func (*versionRows) Close() error      { return nil }
func (r *versionRows) Next(out []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	out[0] = "5.7.44"
	return nil
}

func TestOpenContextCancelsDialectProbeAndRetainsVersionFlags(t *testing.T) {
	t.Run("canceled probe", func(t *testing.T) {
		conn := &versionConnection{block: true, entered: make(chan struct{})}
		pool := sql.OpenDB(versionConnector{conn})
		defer pool.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := initializeContextDatabase(ctx, pool, DriverMySQL); done <- err }()
		select {
		case <-conn.entered:
		case <-time.After(time.Second):
			t.Fatal("version probe did not start")
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("dialect ignored caller cancellation")
		}
		if !conn.closed.Load() {
			t.Fatal("failed initialization retained pool connection")
		}
	})
	t.Run("successful probe", func(t *testing.T) {
		conn := &versionConnection{}
		pool := sql.OpenDB(versionConnector{conn})
		defer pool.Close()
		ctx, cancel := context.WithCancel(context.Background())
		db, err := initializeContextDatabase(ctx, pool, DriverMySQL)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		dialect := db.Dialector.(*mysqlgorm.Dialector)
		if !dialect.DontSupportForShareClause || dialect.ServerVersion != "5.7.44" {
			t.Fatal("dialect version policy skipped")
		}
		if db.ConnPool != pool || db.Statement.ConnPool != pool || dialect.Conn != pool {
			t.Fatal("initialization context retained by database")
		}
		var version string
		if err := db.Raw("SELECT VERSION()").Row().Scan(&version); err != nil || version != "5.7.44" {
			t.Fatal("later query reused canceled initialization context", err)
		}
	})
}

func TestOpenContextSQLiteDefaultsAndCanceledInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sqlite.db")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenWithDriverContext(canceled, DriverSQLite, path); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled open touched database file", err)
	}
	ctx, stop := context.WithCancel(context.Background())
	db, err := OpenWithDriverContext(ctx, DriverSQLite, path)
	stop()
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if pool.Stats().MaxOpenConnections != 1 {
		t.Fatal("SQLite pool changed")
	}
	var fk, busy int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&fk).Error; err != nil || fk != 1 {
		t.Fatal("foreign keys disabled", err)
	}
	if err := db.Raw("PRAGMA busy_timeout").Scan(&busy).Error; err != nil || busy != 5000 {
		t.Fatal("busy timeout changed", busy, err)
	}
	if err := db.Exec("CREATE TABLE times (value DATETIME)").Error; err != nil {
		t.Fatal(err)
	}
	expected := time.Date(2026, 9, 12, 1, 2, 3, 123000000, time.UTC)
	if err := db.Exec("INSERT INTO times (value) VALUES (?)", expected).Error; err != nil {
		t.Fatal(err)
	}
	var got time.Time
	if err := pool.QueryRow("SELECT value FROM times").Scan(&got); err != nil || !got.Equal(expected) {
		t.Fatal("time conversion changed", got, err)
	}
}
