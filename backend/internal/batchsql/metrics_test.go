package batchsql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/libtnb/sqlite"
	mysqldriver "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type metricRow struct {
	ID   uint `gorm:"primaryKey"`
	Name string
	Body []byte
}

func (metricRow) TableName() string { return "batch_sql_rows" }
func sqlFixture(t *testing.T, mysql bool) *gorm.DB {
	t.Helper()
	var dialector gorm.Dialector
	if !mysql {
		dialector = sqlite.Open(filepath.Join(t.TempDir(), "observed.db"))
	} else {
		raw := os.Getenv("ZBOARD_TEST_MYSQL_DSN")
		if raw == "" {
			t.Skip("actual isolated MySQL DSN required")
		}
		c, e := mysqlconfig.ParseDSN(raw)
		if e != nil {
			t.Fatal(e)
		}
		c.DBName = ""
		c.ParseTime = true
		c.Loc = time.UTC
		admin, e := sql.Open("mysql", c.FormatDSN())
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { admin.Close() })
		name := "zboard_batchsql_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if _, e := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			if _, e := admin.Exec("DROP DATABASE `" + name + "`"); e != nil {
				t.Error(e)
			}
		})
		c.DBName = name
		dialector = mysqldriver.Open(c.FormatDSN())
	}
	db, e := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	if e := db.AutoMigrate(&metricRow{}); e != nil {
		t.Fatal(e)
	}
	if !Install(db) || !Install(db) {
		t.Fatal("callback installation failed or was not idempotent")
	}
	return db
}
func TestSQLiteNativeBatchSQLCountsActualSixProcessorsAndRollback(t *testing.T) {
	actualSQLCounts(t, false)
}
func TestMySQLNativeBatchSQLCountsActualSixProcessorsAndRollback(t *testing.T) {
	actualSQLCounts(t, true)
}
func actualSQLCounts(t *testing.T, mysql bool) {
	db := sqlFixture(t, mysql)
	var metrics Metrics
	metrics.Configure(true)
	ctx, c := metrics.Begin(t.Context(), uint64(len("source-payload")))
	row := metricRow{Name: "private-canary", Body: []byte("private-blob")}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Create(&row).Error; e != nil {
			return e
		}
		if e := tx.Model(&row).Update("name", "updated-secret").Error; e != nil {
			return e
		}
		var got metricRow
		if e := tx.First(&got, row.ID).Error; e != nil {
			return e
		}
		var n int
		if e := tx.Raw("SELECT 1").Row().Scan(&n); e != nil {
			return e
		}
		if e := tx.Exec("UPDATE batch_sql_rows SET name = ? WHERE id = ?", "raw-secret", row.ID).Error; e != nil {
			return e
		}
		return tx.Delete(&row).Error
	})
	c.Finish(err, true)
	if err != nil {
		t.Fatal(err)
	}
	snap := metrics.Snapshot(time.Now().UTC(), 30*time.Second)
	if !snap.Known || snap.Batches != 1 || len(snap.Samples) != 1 {
		t.Fatal(snap)
	}
	s := snap.Samples[0]
	if s.Statements != 6 || s.Reads != 2 || s.Writes != 4 || s.UnknownStatements != 0 || s.CommitConfirmed == nil || !*s.CommitConfirmed || s.AffectedWriteRows == nil || *s.AffectedWriteRows != 4 || s.UnknownResults != 1 || s.CallbackNS == 0 || s.TextBlobArgumentBytes == 0 || s.SourcePayloadBytes != uint64(len("source-payload")) {
		t.Fatal("actual callback counts mismatch", s)
	}
	raw, _ := json.Marshal(snap)
	for _, secret := range []string{"private-canary", "private-blob", "updated-secret", "raw-secret", "SELECT", "UPDATE batch_sql_rows"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("diagnostics retained SQL or values", secret)
		}
	}
	ctx, c = metrics.Begin(t.Context(), 0)
	sentinel := errors.New("business rollback")
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Create(&metricRow{Name: "rollback-only", Body: []byte("rollback-blob")}).Error; e != nil {
			return e
		}
		return sentinel
	})
	c.Finish(err, true)
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	var rows int64
	if e := db.Model(&metricRow{}).Count(&rows).Error; e != nil || rows != 0 {
		t.Fatal("actual transaction did not roll back", rows, e)
	}
	snap = metrics.Snapshot(time.Now().UTC(), 30*time.Second)
	failed := snap.Samples[1]
	if failed.CommitConfirmed != nil || failed.Outcome != "transaction_error_or_commit_unknown" || failed.ObservedAffectedWriteRows != 1 || snap.CommittedBatches != 1 || snap.ErrorOrUnknownBatches != 1 {
		t.Fatal("attempted write became committed or known rollback", snap)
	}
}
func TestNativeBatchSQLNormalContextDoesNotAllocateOrRecord(t *testing.T) {
	tx := &gorm.DB{Statement: &gorm.Statement{Context: context.Background()}}
	a, b := before("raw"), after("raw")
	if n := testing.AllocsPerRun(1000, func() { a(tx); b(tx) }); n != 0 {
		t.Fatal("unobserved SQL allocated", n)
	}
	db := sqlFixture(t, false)
	var metrics Metrics
	metrics.Configure(true)
	if e := db.Create(&metricRow{Name: "unobserved"}).Error; e != nil {
		t.Fatal(e)
	}
	if s := metrics.Snapshot(time.Now(), 30*time.Second); s.Known || s.Batches != 0 || len(s.Samples) != 0 {
		t.Fatal("ordinary ORM context was attributed to a batch", s)
	}
}

type panicValueContext struct{ context.Context }

func (panicValueContext) Value(any) any { panic("observer lookup fixture") }
func TestNativeBatchSQLObserverPanicCannotChangeBusinessTransaction(t *testing.T) {
	db := sqlFixture(t, false)
	var metrics Metrics
	metrics.Configure(true)
	if e := db.WithContext(panicValueContext{t.Context()}).Exec("INSERT INTO batch_sql_rows(name,body) VALUES(?,?)", "survives-observer", []byte("x")).Error; e != nil {
		t.Fatal("observer changed financial return", e)
	}
	var n int64
	if e := db.Model(&metricRow{}).Where("name = ?", "survives-observer").Count(&n).Error; e != nil || n != 1 {
		t.Fatal(n, e)
	}
	if s := metrics.Snapshot(time.Now(), time.Minute); s.CallbackFailures == 0 || s.Known {
		t.Fatal("callback failure was advertised complete", s)
	}
}
func TestNativeBatchSQLFixedRingFreshnessConcurrentCollectorsAndNewInstance(t *testing.T) {
	db := sqlFixture(t, false)
	var metrics Metrics
	metrics.Configure(true)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				ctx, c := metrics.Begin(t.Context(), 1)
				e := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					return tx.Exec("UPDATE batch_sql_rows SET name = ? WHERE id = ?", "none", 0).Error
				})
				c.Finish(e, true)
			}
		}()
	}
	wg.Wait()
	now := time.Now().UTC()
	s := metrics.Snapshot(now, time.Minute)
	if s.RetainedBatchCount != SampleCapacity || len(s.Samples) != SampleCapacity || s.Batches != 80 || s.Totals.Statements != 80 || s.Samples[0].Sequence != 17 {
		t.Fatal("unbounded/mixed batch observations", s)
	}
	stale := metrics.Snapshot(now.Add(2*time.Minute), time.Minute)
	if stale.Known || stale.FreshBatchCount != 0 || len(stale.Samples) != 0 || stale.ObservedAt == nil {
		t.Fatal("stale metrics became zero fresh load", stale)
	}
	var restarted Metrics
	restarted.Configure(Install(db))
	empty := restarted.Snapshot(now, time.Minute)
	if empty.Known || empty.Batches != 0 || empty.ObservedAt != nil {
		t.Fatal("new instance inherited old measurements", empty)
	}
}

func TestNativeBatchSQLMalformedObserverStateCannotChangeScopedCommit(t *testing.T) {
	db := sqlFixture(t, false)
	var metrics Metrics
	metrics.Configure(true)
	if e := db.Callback().Raw().After("zboard:batchsql:before:raw").Before("gorm:raw").Register("test:damage_only_observer_state", func(tx *gorm.DB) { tx.Statement.Settings.Store(statementStartKey, "malformed observer fixture") }); e != nil {
		t.Fatal(e)
	}
	defer db.Callback().Raw().Remove("test:damage_only_observer_state")
	ctx, c := metrics.Begin(t.Context(), 0)
	e := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Exec("INSERT INTO batch_sql_rows(name,body) VALUES(?,?)", "scoped-survives", []byte("x")).Error
	})
	c.Finish(e, true)
	if e != nil {
		t.Fatal("observation error changed transaction", e)
	}
	var n int64
	if e := db.Model(&metricRow{}).Where("name = ?", "scoped-survives").Count(&n).Error; e != nil || n != 1 {
		t.Fatal(n, e)
	}
	snap := metrics.Snapshot(time.Now(), time.Minute)
	if snap.Known || snap.IncompleteBatches != 1 || len(snap.Samples) != 1 || snap.Samples[0].CommitConfirmed == nil || !*snap.Samples[0].CommitConfirmed {
		t.Fatal("observer failure fabricated rollback or complete counts", snap)
	}
}
