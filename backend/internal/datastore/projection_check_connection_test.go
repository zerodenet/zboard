package datastore

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectionCheckSQLiteConnectionParameters(t *testing.T) {
	for _, source := range []string{filepath.Join(t.TempDir(), "prepared file.db"), "file:relative.db?mode=rwc&_txlock=immediate"} {
		dsn, err := projectionCheckSQLiteDSN(source)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		v := u.Query()
		if v.Get("mode") != "ro" || v.Get("_txlock") != "deferred" || strings.Contains(dsn, "journal_mode") || strings.Contains(dsn, "synchronous") {
			t.Fatal("check retained writable SQLite setup")
		}
		if strings.Join(v["_pragma"], ",") != "query_only(1),busy_timeout(5000)" {
			t.Fatal("unexpected check-only connection pragmas")
		}
	}
	for _, source := range []string{":memory:", "file::memory:", "file:foo?mode=memory", "db?_pragma=journal_mode(WAL)", "file://remotehost/path", "db?immutable=1", "db?nolock=1"} {
		if _, err := projectionCheckSQLiteDSN(source); !errors.Is(err, ErrProjectionValidationUnsupported) {
			t.Fatal("unsafe SQLite check source accepted", err)
		}
	}
}
func TestProjectionCheckMissingSQLiteFileDoesNotCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")
	db, err := OpenProjectionCheckDatabase(t.Context(), DriverSQLite, path, DefaultPoolConfig())
	if err == nil {
		if pool, e := db.DB(); e == nil {
			_ = pool.Close()
		}
		t.Fatal("missing file opened")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("check created missing SQLite file")
	}
}
