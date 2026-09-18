package networkstore

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

func administrationFixture(t *testing.T) (*gorm.DB, *gorm.DB) {
	t.Helper()
	source := os.Getenv("ZBOARD_TEST_MYSQL_DSN")
	var a, b *gorm.DB
	var err error
	if source == "" {
		path := filepath.Join(t.TempDir(), "competing.db")
		a, err = datastore.OpenWithDriver(datastore.DriverSQLite, path)
		if err != nil {
			t.Fatal(err)
		}
		b, err = datastore.OpenWithDriver(datastore.DriverSQLite, path)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		config, err := mysqlconfig.ParseDSN(source)
		if err != nil {
			t.Fatal(err)
		}
		config.DBName = ""
		config.ParseTime = true
		config.Loc = time.UTC
		admin, err := sql.Open("mysql", config.FormatDSN())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { admin.Close() })
		name := "zboard_network_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if _, err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := admin.Exec("DROP DATABASE `" + name + "`"); err != nil {
				t.Error(err)
			}
		})
		config.DBName = name
		a, err = datastore.Open(config.FormatDSN())
		if err != nil {
			t.Fatal(err)
		}
		b, err = datastore.Open(config.FormatDSN())
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, db := range []*gorm.DB{a, b} {
		pool, _ := db.DB()
		t.Cleanup(func() { pool.Close() })
	}
	if err := datastore.RunMigrations(a); err != nil {
		t.Fatal(err)
	}
	// Production route initialization installs the legacy event projection schema.
	if err := datastore.ReconcileZeroEventSchema(a); err != nil {
		t.Fatal(err)
	}
	return a, b
}
