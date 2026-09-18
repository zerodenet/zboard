package datastore

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/libtnb/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// initializationPool supplies the caller context to dialect version probes,
// whose upstream implementations otherwise use context.Background(). It is
// removed before returning the database; later operations own their contexts.
type initializationPool struct {
	*sql.DB
	ctx context.Context
}

func (p initializationPool) QueryRowContext(_ context.Context, query string, args ...any) *sql.Row {
	return p.DB.QueryRowContext(p.ctx, query, args...)
}
func (p initializationPool) GetDBConn() (*sql.DB, error) { return p.DB, nil }

// OpenWithDriverContext owns connection setup, ping and dialect initialization.
// The returned pool does not retain the initialization context or its deadline.
func OpenWithDriverContext(ctx context.Context, driver, dataSource string, options ...PoolConfig) (db *gorm.DB, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	driver = strings.ToLower(strings.TrimSpace(driver))
	var pool *sql.DB
	switch driver {
	case DriverMySQL:
		pool, err = sql.Open("mysql", dataSource)
	case DriverSQLite:
		pool, err = sql.Open(sqlite.DriverName, sqliteContextDSN(dataSource))
	default:
		return nil, fmt.Errorf("unsupported database driver %q", driver)
	}
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			pool.Close()
		}
	}()
	config := DefaultPoolConfig()
	if len(options) > 0 {
		config = options[0]
	}
	if driver == DriverSQLite {
		config = PoolConfig{MaxOpenConnections: 1, MaxIdleConnections: 1}
	}
	pool.SetMaxOpenConns(config.MaxOpenConnections)
	pool.SetMaxIdleConns(config.MaxIdleConnections)
	pool.SetConnMaxLifetime(config.ConnectionLifetime)
	if err = pool.PingContext(ctx); err != nil {
		return nil, err
	}
	db, err = initializeContextDatabase(ctx, pool, driver)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return db, nil
}
func initializeContextDatabase(ctx context.Context, pool *sql.DB, driver string) (*gorm.DB, error) {
	init := initializationPool{DB: pool, ctx: ctx}
	var dialect gorm.Dialector
	if driver == DriverMySQL {
		dialect = mysql.New(mysql.Config{Conn: init})
	} else {
		dialect = sqlite.New(sqlite.Config{Conn: init})
	}
	db, err := gorm.Open(dialect, &gorm.Config{DisableAutomaticPing: true, NowFunc: func() time.Time { return time.Now().UTC() }})
	if err != nil {
		return nil, err
	}
	db.ConnPool = pool
	db.Statement.ConnPool = pool
	switch d := dialect.(type) {
	case *mysql.Dialector:
		d.Conn = pool
	case *sqlite.Dialector:
		d.Conn = pool
	}
	return db, nil
}

// Supplying Conn skips libtnb/sqlite's DSN injection. Retain its v1.2.2 time
// conversion defaults here, alongside ZBoard's existing pragma/locking policy.
func sqliteContextDSN(dataSource string) string {
	dsn := normalizeSQLiteDSN(dataSource)
	path, raw, _ := strings.Cut(dsn, "?")
	values, err := url.ParseQuery(raw)
	if path == "" || err != nil {
		return dsn
	}
	for _, option := range []struct{ key, value string }{{"_texttotime", "1"}, {"_inttotime", "1"}, {"_time_format", "sqlite"}} {
		if !values.Has(option.key) {
			dsn += "&" + option.key + "=" + option.value
		}
	}
	return dsn
}
