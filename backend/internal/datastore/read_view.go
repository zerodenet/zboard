package datastore

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/libtnb/sqlite"
	"gorm.io/gorm"
)

// OpenReadView separates SQLite reporting from its single writer connection.
// A read-only, deferred WAL transaction can retain a consistent snapshot while
// the writer commits. In-memory/non-WAL databases and MySQL reuse their pool.
// The caller owns the returned close function and must stop reads before closing.
func OpenReadView(db *gorm.DB) (*gorm.DB, func() error, error) {
	unchanged := func() (*gorm.DB, func() error, error) { return db, func() error { return nil }, nil }
	if !IsSQLite(db) {
		return unchanged()
	}
	var databases []struct {
		Name string
		File string
	}
	if err := db.Raw("PRAGMA database_list").Scan(&databases).Error; err != nil {
		return nil, nil, fmt.Errorf("inspect SQLite reporting database: %w", err)
	}
	path := ""
	for _, database := range databases {
		if database.Name == "main" {
			path = database.File
		}
	}
	if path == "" {
		return unchanged()
	}
	var journal string
	if err := db.Raw("PRAGMA journal_mode").Scan(&journal).Error; err != nil {
		return nil, nil, err
	}
	if !strings.EqualFold(journal, "wal") {
		return unchanged()
	}
	uri := url.URL{Scheme: "file", Path: path}
	parameters := url.Values{"mode": {"ro"}, "_txlock": {"deferred"}, "_pragma": {"query_only(1)", "busy_timeout(5000)"}}
	uri.RawQuery = parameters.Encode()
	view, err := gorm.Open(sqlite.Open(uri.String()), &gorm.Config{Logger: db.Logger, NowFunc: db.NowFunc})
	if err != nil {
		return nil, nil, fmt.Errorf("open SQLite reporting connection: %w", err)
	}
	pool, err := view.DB()
	if err != nil {
		return nil, nil, err
	}
	pool.SetMaxOpenConns(2)
	pool.SetMaxIdleConns(2)
	pool.SetConnMaxLifetime(0)
	return view, pool.Close, nil
}
