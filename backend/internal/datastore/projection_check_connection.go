package datastore

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/libtnb/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenProjectionCheckDatabase opens no services and does not use the normal
// SQLite opener, whose WAL/synchronous pragmas can change persistent settings.
// MySQL keeps the configured DSN unchanged; its caller executes the validator
// in a READ ONLY transaction. SQLite cannot create a missing database file.
func OpenProjectionCheckDatabase(ctx context.Context, driver, source string, options PoolConfig) (db *gorm.DB, err error) {
	if driver != DriverMySQL && driver != DriverSQLite {
		return nil, projectionUnsupported("unsupported check connection driver", nil)
	}
	dsn := source
	sqlDriver := "mysql"
	if driver == DriverSQLite {
		sqlDriver = sqlite.DriverName
		dsn, err = projectionCheckSQLiteDSN(source)
		if err != nil {
			return nil, err
		}
	}
	pool, err := sql.Open(sqlDriver, dsn)
	if err != nil {
		return nil, projectionError("read-only connection open", err)
	}
	defer func() {
		if err != nil {
			_ = pool.Close()
		}
	}()
	if driver == DriverSQLite {
		options = PoolConfig{MaxOpenConnections: 1, MaxIdleConnections: 1}
	}
	pool.SetMaxOpenConns(options.MaxOpenConnections)
	pool.SetMaxIdleConns(options.MaxIdleConnections)
	pool.SetConnMaxLifetime(options.ConnectionLifetime)
	if err = pool.PingContext(ctx); err != nil {
		return nil, projectionError("read-only connection ping", err)
	}
	// Use the existing context-bound initialization pool with a silent logger.
	// Even connection/metadata failures must not print accounts or driver values.
	init := initializationPool{DB: pool, ctx: ctx}
	var dialect gorm.Dialector
	if driver == DriverMySQL {
		dialect = mysql.New(mysql.Config{Conn: init})
	} else {
		dialect = sqlite.New(sqlite.Config{Conn: init})
	}
	db, err = gorm.Open(dialect, &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard, NowFunc: func() time.Time { return time.Now().UTC() }})
	if err == nil {
		db.ConnPool = pool
		db.Statement.ConnPool = pool
		switch d := dialect.(type) {
		case *mysql.Dialector:
			d.Conn = pool
		case *sqlite.Dialector:
			d.Conn = pool
		}
	}
	if err != nil {
		return nil, projectionError("read-only dialect initialization", err)
	}
	return db, nil
}
func projectionCheckSQLiteDSN(source string) (string, error) {
	rawPath, rawQuery, _ := strings.Cut(strings.TrimSpace(source), "?")
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", projectionUnsupported("invalid SQLite check parameters", nil)
	}
	if rawPath == "" || rawPath == ":memory:" || values.Get("mode") == "memory" {
		return "", projectionUnsupported("persistent existing SQLite file required", nil)
	}
	// Immutable/nolock bypass the normal WAL/change-detection or locking
	// contract. A live projection database cannot be certified through them.
	if values.Has("immutable") || values.Has("nolock") {
		return "", projectionUnsupported("SQLite change-detection bypass cannot be certified", nil)
	}
	if values.Has("_pragma") {
		return "", projectionUnsupported("explicit SQLite pragmas cannot be certified read-only", nil)
	}
	path := rawPath
	if strings.HasPrefix(rawPath, "file:") {
		parsed, e := url.Parse(rawPath)
		if e != nil || parsed.Host != "" {
			return "", projectionUnsupported("unsupported SQLite file URI", nil)
		}
		path = parsed.Path
		if path == "" {
			path = parsed.Opaque
		}
	}
	if path == "" || path == ":memory:" {
		return "", projectionUnsupported("persistent existing SQLite file required", nil)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", projectionUnsupported("invalid SQLite file path", nil)
	}
	values.Set("mode", "ro")
	values.Set("_txlock", "deferred")
	values.Set("_pragma", "query_only(1)")
	values.Add("_pragma", "busy_timeout(5000)")
	for _, v := range []struct{ k, v string }{{"_texttotime", "1"}, {"_inttotime", "1"}, {"_time_format", "sqlite"}} {
		if !values.Has(v.k) {
			values.Set(v.k, v.v)
		}
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: values.Encode()}
	return uri.String(), nil
}
