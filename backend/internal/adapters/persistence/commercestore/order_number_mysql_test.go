package commercestore

import (
	"context"
	"fmt"
	"github.com/zerodenet/zboard/backend/migrations"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/businessref"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This deliberately small real-MySQL fixture tests number publication and
// rollback without preparing the full commerce schema or touching live data.
func numberMySQL(t *testing.T) *gorm.DB {
	t.Helper()
	raw := os.Getenv("ZBOARD_NUMBER_TEST_MYSQL_DSN")
	if raw == "" {
		t.Skip("ZBOARD_NUMBER_TEST_MYSQL_DSN not set")
	}
	config, e := driver.ParseDSN(raw)
	if e != nil {
		t.Fatal(e)
	}
	config.DBName = ""
	admin, e := gorm.Open(mysql.Open(config.FormatDSN()), &gorm.Config{Logger: logger.Discard})
	if e != nil {
		t.Fatal(e)
	}
	seed, e := businessref.Provisional('N')
	if e != nil {
		t.Fatal(e)
	}
	schema := "zboard_number_" + strings.ToLower(seed)
	if e := admin.Exec("CREATE DATABASE `" + schema + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci").Error; e != nil {
		t.Fatal(e)
	}
	config.DBName = schema
	db, e := gorm.Open(mysql.Open(config.FormatDSN()), &gorm.Config{Logger: logger.Discard})
	if e != nil {
		t.Fatal(e)
	}
	pool, _ := db.DB()
	pool.SetMaxOpenConns(8)
	pool.SetMaxIdleConns(4)
	t.Cleanup(func() { pool.Close(); admin.Exec("DROP DATABASE `" + schema + "`"); p, _ := admin.DB(); p.Close() })
	if e := db.Exec("CREATE TABLE orders(id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,trade_no VARCHAR(64) NOT NULL,created_at DATETIME(3) NULL,updated_at DATETIME(3) NULL,UNIQUE KEY uk_order_number(trade_no)) ENGINE=InnoDB").Error; e != nil {
		t.Fatal(e)
	}
	return db
}
func TestMySQLBusinessNumberConcurrentCommitAndRollback(t *testing.T) {
	db := numberMySQL(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var group sync.WaitGroup
	failures := make(chan error, 4)
	for range 4 {
		group.Go(func() {
			for range 10 {
				var row model.Order
				e := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return createNumberedOrder(tx.Select("id", "trade_no"), &row) })
				if e != nil {
					failures <- e
					return
				}
				if row.TradeNo != businessref.Order(row.ID, row.CreatedAt) {
					failures <- fmt.Errorf("number mismatch %d %s", row.ID, row.TradeNo)
					return
				}
			}
		})
	}
	group.Wait()
	close(failures)
	for e := range failures {
		t.Fatal(e)
	}
	var counts struct{ Rows, Unique int64 }
	if e := db.Table("orders").Select("COUNT(*) AS `rows`,COUNT(DISTINCT trade_no) AS `unique`").Scan(&counts).Error; e != nil || counts.Rows != 40 || counts.Unique != 40 {
		t.Fatal(counts, e)
	}
	// A failure after INSERT but before the final number publishes must leave no
	// provisional business object. Existing committed numbers stay unchanged.
	if e := db.Exec("CREATE TRIGGER reject_number BEFORE UPDATE ON orders FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='number publication failed'").Error; e != nil {
		t.Fatal(e)
	}
	var failed model.Order
	if e := db.Transaction(func(tx *gorm.DB) error { return createNumberedOrder(tx.Select("id", "trade_no"), &failed) }); e == nil {
		t.Fatal("publication failure ignored")
	}
	var total int64
	if e := db.Table("orders").Count(&total).Error; e != nil || total != 40 {
		t.Fatal("provisional order escaped rollback", total, e)
	}
	t.Log("40 concurrent committed references are unique; failed publication leaves no provisional order")
}

func TestMySQLTicketNumberWidthPreservesIssuedReferences(t *testing.T) {
	db := numberMySQL(t)
	if e := db.Exec("CREATE TABLE tickets(id BIGINT UNSIGNED PRIMARY KEY, ticket_no VARCHAR(32) NOT NULL UNIQUE) ENGINE=InnoDB").Error; e != nil {
		t.Fatal(e)
	}
	legacy := "T20261113-LEGACY01"
	if e := db.Exec("INSERT INTO tickets VALUES(1,?)", legacy).Error; e != nil {
		t.Fatal(e)
	}
	migration, e := migrations.Files.ReadFile("0091_ticket_business_number_width.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if e := db.Exec(string(migration)).Error; e != nil {
		t.Fatal(e)
	}
	number := businessref.Ticket(^uint(0), time.Date(2026, 11, 13, 9, 45, 30, 0, time.UTC))
	if e := db.Exec("INSERT INTO tickets VALUES(2,?)", number).Error; e != nil {
		t.Fatal(e)
	}
	var rows []struct{ TicketNo string }
	if e := db.Table("tickets").Select("ticket_no").Order("id").Scan(&rows).Error; e != nil {
		t.Fatal(e)
	}
	if len(rows) != 2 || rows[0].TicketNo != legacy || rows[1].TicketNo != number {
		t.Fatal(rows)
	}
}
