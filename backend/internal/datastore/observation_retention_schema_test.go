package datastore_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

func TestSQLiteObservationRetentionIndexMigrationAndRecovery(t *testing.T) {
	db, err := datastore.OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "observation-index.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	assertObservationRetentionIndex(t, db)
}

func TestMySQLObservationRetentionIndexMigrationAndRecovery(t *testing.T) {
	db, _ := hourlyMySQLFixture(t)
	assertObservationRetentionIndex(t, db)
}

func assertObservationRetentionIndex(t *testing.T, db *gorm.DB) {
	t.Helper()
	const version = "0056_observation_retention_index.up.sql"
	const index = "idx_fair_use_event_retention"
	const table = "subscription_fair_use_events"
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("fresh database", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := db.Exec("INSERT INTO subscription_fair_use_events (subscription_id,event_type,metrics_json,reason,occurred_at,created_at) VALUES (?,?,?,?,?,?)", 7, "evaluation", `{}`, "preserved explanation", now.Add(-20*24*time.Hour), now).Error; err != nil {
		t.Fatal(err)
	}
	for _, step := range []string{"old-database", "ddl-before-receipt", "recorded-missing-index", "repeat"} {
		if step == "old-database" || step == "ddl-before-receipt" {
			if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
				t.Fatal(err)
			}
		}
		if step == "old-database" || step == "recorded-missing-index" {
			if err := db.Migrator().DropIndex(table, index); err != nil {
				t.Fatal(err)
			}
		}
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal(step, err)
		}
		var count int64
		if err := db.Table(table).Where("subscription_id = ? AND reason = ?", 7, "preserved explanation").Count(&count).Error; err != nil || count != 1 || !db.Migrator().HasIndex(table, index) {
			t.Fatal("index recovery changed existing diagnostics", step, count, err)
		}
		if err := db.Table("schema_migrations").Where("version = ?", version).Count(&count).Error; err != nil || count != 1 {
			t.Fatal("index migration receipt missing or duplicated", count, err)
		}
	}
	query := "SELECT id FROM subscription_fair_use_events WHERE occurred_at < ? ORDER BY occurred_at ASC,id ASC LIMIT 1000"
	if datastore.IsSQLite(db) {
		var plan []struct{ Detail string }
		if err := db.Raw("EXPLAIN QUERY PLAN "+query, now).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		var details []string
		for _, row := range plan {
			details = append(details, row.Detail)
		}
		text := strings.Join(details, " ")
		if !strings.Contains(text, "SEARCH") || !strings.Contains(text, index) || strings.Contains(text, "TEMP B-TREE") {
			t.Fatal("global retention discovery scans or sorts unrelated subjects", text)
		}
	} else {
		var plan []struct {
			Key   string `gorm:"column:key"`
			Extra string
		}
		if err := db.Raw("EXPLAIN "+query, now).Scan(&plan).Error; err != nil || len(plan) != 1 || plan[0].Key != index || strings.Contains(strings.ToLower(plan[0].Extra), "filesort") {
			t.Fatal("global retention discovery lacks the time index", plan, err)
		}
	}
	// A different existing definition is operator-visible; do not drop an
	// unknown index or mark the migration complete around it.
	if err := db.Migrator().DropIndex(table, index); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE INDEX " + index + " ON " + table + " (subscription_id)").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "unexpected columns") {
		t.Fatal("wrong existing index silently accepted", err)
	}
}
