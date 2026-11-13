package observationstore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// Real migration DDL and an isolated disposable schema, never a production DB.
func claimMySQLDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	dsn := os.Getenv("ZBOARD_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set isolated ZBOARD_TEST_MYSQL_DSN")
	}
	cfg, e := mysqlconfig.ParseDSN(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.DBName = ""
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	admin, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("zboard_claim_%d", time.Now().UnixNano())
	if _, e := admin.Exec("CREATE DATABASE `" + schema + "`"); e != nil {
		t.Fatal(e)
	}
	cfg.DBName = schema
	db, e := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	pool, _ := db.DB()
	pool.SetMaxOpenConns(8)
	t.Cleanup(func() { pool.Close(); admin.Exec("DROP DATABASE `" + schema + "`"); admin.Close() })
	raw, e := migrations.Files.ReadFile("0060_node_observation_handoff.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if e := db.Exec(strings.Split(string(raw), ";")[0]).Error; e != nil {
		t.Fatal(e)
	}
	raw, e = migrations.Files.ReadFile("0061_node_observation_material_retention.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, stmt := range strings.Split(string(raw), ";")[:3] {
		if e := db.Exec(stmt).Error; e != nil {
			t.Fatal(e)
		}
	}
	raw, e = migrations.Files.ReadFile("0089_observation_claim_lease.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if e := db.Exec(string(raw)).Error; e != nil {
		t.Fatal(e)
	}
	return db, schema
}
func claimRow(id string, status string, at time.Time) model.NodeObservationIntent {
	return model.NodeObservationIntent{ID: id, NodeID: 1, AssetTag: strings.Repeat("a", 64), Status: status, AvailableAt: at, LeaseUntil: at, CreatedAt: at, UpdatedAt: at, MaterialReviewAt: at}
}

func TestMySQLObservationClaimLocksOnlyExactCandidate(t *testing.T) {
	db, schema := claimMySQLDB(t)
	now := clockNow()
	rows := []model.NodeObservationIntent{claimRow("due", "pending", now.Add(-time.Hour))}
	for i := 0; i < 2000; i++ {
		rows = append(rows, claimRow(fmt.Sprintf("future-%05d", i), "pending", now.Add(time.Hour)))
	}
	if e := db.CreateInBatches(rows, 100).Error; e != nil {
		t.Fatal(e)
	}
	locked, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var once atomic.Bool
	const callback = "test:hold_claim_pk"
	db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Clauses["FOR"]; ok && tx.Statement.Table == "node_observation_intents" && tx.RowsAffected == 1 && once.CompareAndSwap(false, true) {
			close(locked)
			select {
			case <-release:
			case <-ctx.Done():
				tx.AddError(ctx.Err())
			}
		}
	})
	defer db.Callback().Query().Remove(callback)
	type result struct {
		h     model.NodeObservationIntent
		found bool
		err   error
	}
	done := make(chan result, 1)
	go func() { h, ok, e := (Worker{DB: db}).claim(ctx); done <- result{h, ok, e} }()
	select {
	case <-locked:
	case <-ctx.Done():
		t.Fatal("claim did not acquire its candidate", ctx.Err())
	}
	var locks int64
	e := db.Raw("SELECT COUNT(*) FROM performance_schema.data_locks WHERE OBJECT_SCHEMA=? AND OBJECT_NAME='node_observation_intents' AND LOCK_TYPE='RECORD'", schema).Scan(&locks).Error
	probe := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var h model.NodeObservationIntent
		return tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "NOWAIT"}).First(&h, "id=?", "future-00000").Error
	})
	close(release)
	r := <-done
	if e != nil || locks > 4 || probe != nil || r.err != nil || !r.found || r.h.ID != "due" {
		t.Fatalf("locks=%d probe=%v result=%+v error=%v", locks, probe, r, e)
	}
	t.Logf("2000 not-yet-due tasks, one due task: %d record locks; unrelated row remains writable", locks)
}
func TestMySQLObservationClaimSkipsBusyAndReclaimsExpiredLease(t *testing.T) {
	db, _ := claimMySQLDB(t)
	now := clockNow()
	rows := []model.NodeObservationIntent{claimRow("busy", "pending", now.Add(-3*time.Hour)), claimRow("expired", "claimed", now.Add(-2*time.Hour)), claimRow("pending", "pending", now.Add(-time.Hour))}
	rows[1].Token = "old-owner"
	rows[1].Attempts = 2
	if e := db.Create(&rows).Error; e != nil {
		t.Fatal(e)
	}
	held := db.Begin()
	defer held.Rollback()
	var h model.NodeObservationIntent
	if e := held.Clauses(clause.Locking{Strength: "UPDATE"}).First(&h, "id=?", "busy").Error; e != nil {
		t.Fatal(e)
	}
	got, ok, e := (Worker{DB: db}).claim(t.Context())
	if e != nil || !ok || got.ID != "expired" || got.Token == "old-owner" || got.Attempts != 3 || !got.LeaseUntil.After(now) {
		t.Fatal(got, ok, e)
	}
	// A stale holder cannot acknowledge the newly owned lease.
	if e := db.Transaction(func(tx *gorm.DB) error { return ownedAck(tx, rows[1], "done", "") }); e != ErrOwnership {
		t.Fatal("old lease retained authority", e)
	}
	got, ok, e = (Worker{DB: db}).claim(t.Context())
	if e != nil || !ok || got.ID != "pending" {
		t.Fatal(got, ok, e)
	}
}
