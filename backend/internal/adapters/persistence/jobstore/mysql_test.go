package jobstore

import (
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/datastore"
)

func mysqlFixture(t *testing.T, capacity int) (*Store, *Store) {
	t.Helper()
	c, err := mysqlconfig.ParseDSN(os.Getenv("ZBOARD_TEST_MYSQL_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	c.DBName = ""
	c.ParseTime = true
	c.Loc = time.UTC
	admin, err := sql.Open("mysql", c.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	name := "zboard_jobs_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE `" + name + "`"); err != nil {
			t.Error(err)
		}
	})
	c.DBName = name
	a, err := datastore.Open(c.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	b, err := datastore.Open(c.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Store{New(a), New(b)} {
		pool, _ := s.db.DB()
		t.Cleanup(func() { pool.Close() })
	}
	if err := datastore.RunMigrations(a); err != nil {
		t.Fatal(err)
	}
	if err := a.Model(&Budget{}).Where("id = 1").Update("capacity", capacity).Error; err != nil {
		t.Fatal(err)
	}
	return New(a), New(b)
}
