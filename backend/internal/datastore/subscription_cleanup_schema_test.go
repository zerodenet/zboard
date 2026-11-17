package datastore

import (
	"crypto/rand"
	"encoding/hex"
	driver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"path/filepath"
	"testing"
)

func TestSQLiteSubscriptionCleanupDetachesOnlyHistoricalReferences(t *testing.T) {
	db, e := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "subscription-cleanup.db"))
	if e != nil {
		t.Fatal(e)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	assertSubscriptionCleanupForeignKeys(t, db)
}
func assertSubscriptionCleanupForeignKeys(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, sql := range []string{
		"CREATE TABLE schema_migrations(version VARCHAR(191) PRIMARY KEY,applied_at datetime)",
		"CREATE TABLE account_subjects(id INTEGER PRIMARY KEY)",
		"CREATE TABLE system_configs(config_key VARCHAR(100),description TEXT)",
		"CREATE TABLE subscriptions(id INTEGER PRIMARY KEY)",
		"CREATE TABLE orders(id INTEGER PRIMARY KEY,target_subscription_id INTEGER, CONSTRAINT fk_orders_target_subscription FOREIGN KEY(target_subscription_id) REFERENCES subscriptions(id) ON DELETE SET NULL)",
		"CREATE TABLE protocol_credentials(id INTEGER PRIMARY KEY,subscription_id INTEGER, CONSTRAINT fk_protocol_credentials_subscription FOREIGN KEY(subscription_id) REFERENCES subscriptions(id) ON DELETE CASCADE)",
		"CREATE TABLE flow_usages(id INTEGER PRIMARY KEY,subscription_id INTEGER, CONSTRAINT fk_flow_usages_subscription FOREIGN KEY(subscription_id) REFERENCES subscriptions(id) ON DELETE CASCADE)",
		"CREATE TABLE subscription_value_settlements(id INTEGER PRIMARY KEY,subscription_id INTEGER, CONSTRAINT fk_subscription_value_settlements_subscription FOREIGN KEY(subscription_id) REFERENCES subscriptions(id) ON DELETE RESTRICT)",
		"CREATE TABLE subscription_tokens(id INTEGER PRIMARY KEY,subscription_id INTEGER, CONSTRAINT fk_token_live_subscription FOREIGN KEY(subscription_id) REFERENCES subscriptions(id) ON DELETE CASCADE)",
		"INSERT INTO subscriptions VALUES(1)",
		"INSERT INTO orders VALUES(1,1)", "INSERT INTO protocol_credentials VALUES(1,1)", "INSERT INTO flow_usages VALUES(1,1)", "INSERT INTO subscription_value_settlements VALUES(1,1)", "INSERT INTO subscription_tokens VALUES(1,1)",
	} {
		if e := db.Exec(sql).Error; e != nil {
			t.Fatal(sql, e)
		}
	}
	for range 2 {
		if e := reconcileSubscriptionCleanupSchema(db); e != nil {
			t.Fatal(e)
		}
	}
	if e := db.Exec("DELETE FROM subscriptions WHERE id=1").Error; e != nil {
		t.Fatal(e)
	}
	for table, column := range subscriptionHistoryReferences {
		var n int64
		if e := db.Table(table).Where(column + "=1").Count(&n).Error; e != nil || n != 1 {
			t.Fatal("historical attribution was cascaded or nulled", table, n, e)
		}
	}
	var n int64
	if e := db.Table("subscription_tokens").Count(&n).Error; e != nil || n != 0 {
		t.Fatal("runtime foreign key no longer enforced", n, e)
	}
	if e := db.Exec("ALTER TABLE account_subjects DROP COLUMN last_entitlement_end").Error; e != nil {
		t.Fatal(e)
	}
	if e := reconcileSubscriptionCleanupSchema(db); e == nil {
		t.Fatal("recorded anchor loss was silently rebuilt")
	}
}

func TestMySQLSubscriptionCleanupDetachesOnlyHistoricalReferences(t *testing.T) {
	raw := os.Getenv("ZBOARD_TEST_MYSQL_DSN")
	if raw == "" {
		t.Skip("ZBOARD_TEST_MYSQL_DSN not set")
	}
	cfg, e := driver.ParseDSN(raw)
	if e != nil {
		t.Fatal(e)
	}
	cfg.DBName = ""
	admin, e := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Discard})
	if e != nil {
		t.Fatal(e)
	}
	seed := make([]byte, 8)
	if _, e := rand.Read(seed); e != nil {
		t.Fatal(e)
	}
	name := "zboard_sub_cleanup_" + hex.EncodeToString(seed)
	if e := admin.Exec("CREATE DATABASE `" + name + "`").Error; e != nil {
		t.Fatal(e)
	}
	cfg.DBName = name
	db, e := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Discard})
	if e != nil {
		t.Fatal(e)
	}
	pool, _ := db.DB()
	ap, _ := admin.DB()
	t.Cleanup(func() { pool.Close(); admin.Exec("DROP DATABASE `" + name + "`"); ap.Close() })
	assertSubscriptionCleanupForeignKeys(t, db)
}
