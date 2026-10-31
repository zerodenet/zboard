package meteringstore

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func usageProjectionDB(t testing.TB, path string) *gorm.DB {
	t.Helper()
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, path)
	if err != nil {
		t.Fatal(err)
	}
	db.Logger = logger.Discard
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&model.TrafficRecord{}, &model.MeteringReportReceipt{}, &model.ProtocolEndpointUsageDaily{}, &model.MeteringUsageProjectionPending{}); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasColumn("traffic_records", "entitlement_epoch") {
		if err := db.Exec("ALTER TABLE traffic_records ADD COLUMN entitlement_epoch bigint NOT NULL DEFAULT 0").Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, spec := range []struct{ table, name, kind, initial string }{
		{"traffic_records", "is_aggregate", "numeric", "false"}, {"traffic_records", "record_count", "integer", "1"},
		{"metering_usage_projection_pending", "delta_raw_bytes", "integer", "0"}, {"metering_usage_projection_pending", "delta_upload_bytes", "integer", "0"}, {"metering_usage_projection_pending", "delta_download_bytes", "integer", "0"}, {"metering_usage_projection_pending", "delta_used_bytes", "integer", "0"}, {"metering_usage_projection_pending", "delta_record_count", "integer", "0"},
	} {
		if !db.Migrator().HasColumn(spec.table, spec.name) {
			if err := db.Exec("ALTER TABLE " + spec.table + " ADD COLUMN " + spec.name + " " + spec.kind + " NOT NULL DEFAULT " + spec.initial).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if !db.Migrator().HasTable("traffic_usage_hourly") {
		if err := datastore.RebuildTrafficHourly(db); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestUsageProjectionCompetingSQLiteWorkersSerializeAndRecover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "competing-projections.db")
	a := usageProjectionDB(t, path)
	b := usageProjectionDB(t, path)
	for i := uint(1); i <= 4; i++ {
		enqueueProjectionRecord(t, a, i, 11)
	}
	firstRead := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	if err := a.Callback().Query().After("gorm:query").Register("test:hold_pending_snapshot", func(tx *gorm.DB) {
		if tx.Statement.Table == "metering_usage_projection_pending" {
			once.Do(func() { close(firstRead); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	secondRead := make(chan struct{}, 1)
	if err := b.Callback().Query().After("gorm:query").Register("test:observe_pending_snapshot", func(tx *gorm.DB) {
		if tx.Statement.Table == "metering_usage_projection_pending" {
			secondRead <- struct{}{}
		}
	}); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		count int
		err   error
	}
	done := make(chan outcome, 2)
	go func() {
		count, err := (UsageProjectionWorker{DB: a}).Drain(context.Background(), 2)
		done <- outcome{count, err}
	}()
	select {
	case <-firstRead:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("first worker did not read pending")
	}
	go func() {
		count, err := (UsageProjectionWorker{DB: b}).Drain(context.Background(), 2)
		done <- outcome{count, err}
	}()
	// OpenWithDriver uses BEGIN IMMEDIATE. The second worker acquires its
	// connection but cannot establish a stale pending snapshot before the first
	// commits. Verify actual production-driver serialization, not a mocked lock.
	pool, _ := b.DB()
	deadline := time.Now().Add(time.Second)
	for pool.Stats().InUse == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	select {
	case <-secondRead:
		close(release)
		t.Fatal("second worker read pending before SQLite write-lock release")
	default:
	}
	close(release)
	for i := 0; i < 2; i++ {
		result := <-done
		if result.err != nil || result.count != 2 {
			t.Fatalf("competing worker: %d %v", result.count, result.err)
		}
	}
	projectionCounts(t, a, 4, 0, 44)
	if count, err := (UsageProjectionWorker{DB: a}).Drain(context.Background(), 2); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func enqueueProjectionRecord(t *testing.T, db *gorm.DB, id uint, used int64) {
	t.Helper()
	if err := db.Transaction(func(tx *gorm.DB) error {
		record := model.TrafficRecord{ID: id, NodeID: 1, ProtocolEndpointID: 3, ReportID: time.Now().String(), Nonce: time.Now().String(), At: time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC), RawBytes: 7, UploadBytes: 3, DownloadBytes: 4, ProtocolMultiplierMilli: 1500, UsedBytes: used}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		return EnqueueUsageProjections(tx, []model.TrafficRecord{record})
	}); err != nil {
		t.Fatal(err)
	}
}

func projectionCounts(t *testing.T, db *gorm.DB, wantFacts, wantPending int64, wantUsed int64) {
	t.Helper()
	for _, check := range []struct {
		model any
		want  int64
	}{{&model.TrafficRecord{}, wantFacts}, {&model.MeteringUsageProjectionPending{}, wantPending}} {
		var count int64
		if err := db.Model(check.model).Count(&count).Error; err != nil || count != check.want {
			t.Fatalf("%T count=%d want=%d err=%v", check.model, count, check.want, err)
		}
	}
	var used int64
	if err := db.Model(&model.ProtocolEndpointUsageDaily{}).Select("COALESCE(SUM(used_bytes),0)").Scan(&used).Error; err != nil || used != wantUsed {
		t.Fatalf("projection used=%d want=%d err=%v", used, wantUsed, err)
	}
}

func TestUsageProjectionFailureKeepsSettledFactsAndRecoversAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage-projection.db")
	db := usageProjectionDB(t, path)
	if err := db.Exec("CREATE TABLE settled_quota (id integer PRIMARY KEY, used_bytes bigint NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO settled_quota VALUES (1, 11)").Error; err != nil {
		t.Fatal(err)
	}
	enqueueProjectionRecord(t, db, 1, 11)
	if err := db.Exec(`CREATE TRIGGER reject_daily BEFORE INSERT ON protocol_endpoint_usage_daily BEGIN SELECT RAISE(ABORT, 'chart unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	worker := UsageProjectionWorker{DB: db}
	if count, err := worker.Drain(context.Background(), 100); err == nil || count != 0 {
		t.Fatalf("failed projection acknowledged count=%d err=%v", count, err)
	}
	projectionCounts(t, db, 1, 1, 0)
	var used int64
	if err := db.Table("settled_quota").Select("used_bytes").Scan(&used).Error; err != nil || used != 11 {
		t.Fatalf("chart failure changed settled quota: %d %v", used, err)
	}
	if err := db.Exec("DROP TRIGGER reject_daily").Error; err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	// Reopen the file and construct a new worker: recovery uses only durable
	// pending rows and ledger values, never an in-memory queue/cursor.
	db = usageProjectionDB(t, path)
	worker = UsageProjectionWorker{DB: db}
	if pending, err := worker.HasPending(context.Background()); err != nil || !pending {
		t.Fatal(pending, err)
	}
	if count, err := worker.Drain(context.Background(), 100); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	projectionCounts(t, db, 1, 0, 11)
	if count, err := worker.Drain(context.Background(), 100); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	projectionCounts(t, db, 1, 0, 11)
}

func TestUsageProjectionCheckpointFailureRollsBackEveryDelta(t *testing.T) {
	db := usageProjectionDB(t, filepath.Join(t.TempDir(), "checkpoint.db"))
	enqueueProjectionRecord(t, db, 1, 11)
	enqueueProjectionRecord(t, db, 2, 10)
	if err := db.Exec(`CREATE TRIGGER reject_checkpoint BEFORE DELETE ON metering_usage_projection_pending BEGIN SELECT RAISE(ABORT, 'checkpoint unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	worker := UsageProjectionWorker{DB: db}
	if count, err := worker.Drain(context.Background(), 100); err == nil || count != 0 {
		t.Fatal(count, err)
	}
	projectionCounts(t, db, 2, 2, 0)
	if err := db.Exec("DROP TRIGGER reject_checkpoint").Error; err != nil {
		t.Fatal(err)
	}
	if count, err := worker.Drain(context.Background(), 100); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	projectionCounts(t, db, 2, 0, 21)
	var row model.ProtocolEndpointUsageDaily
	if err := db.First(&row).Error; err != nil || row.RawBytes != 14 || row.UploadBytes != 6 || row.DownloadBytes != 8 || row.UsedBytes != 21 || row.RecordCount != 2 {
		t.Fatalf("projection must sum original settled deltas without rerounding: %+v %v", row, err)
	}
}

func TestUsageProjectionBoundedRecoveryDoesNotSkipLateLowerID(t *testing.T) {
	db := usageProjectionDB(t, filepath.Join(t.TempDir(), "late.db"))
	worker := UsageProjectionWorker{DB: db}
	enqueueProjectionRecord(t, db, 100, 11)
	if count, err := worker.Drain(context.Background(), 1); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	// Model a lower auto-increment ID whose transaction commits later.
	enqueueProjectionRecord(t, db, 2, 10)
	enqueueProjectionRecord(t, db, 101, 12)
	if count, err := worker.Drain(context.Background(), 1); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	projectionCounts(t, db, 3, 1, 21)
	if count, err := worker.Drain(context.Background(), 1); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	projectionCounts(t, db, 3, 0, 33)
	for _, limit := range []int{0, -1, UsageProjectionBatchLimit + 1} {
		if _, err := worker.Drain(context.Background(), limit); err == nil {
			t.Fatalf("accepted unbounded/invalid limit %d", limit)
		}
	}
}

func TestUsageProjectionIntentRollsBackWithFact(t *testing.T) {
	db := usageProjectionDB(t, filepath.Join(t.TempDir(), "rollback.db"))
	abort := errors.New("quota commit failed")
	err := db.Transaction(func(tx *gorm.DB) error {
		record := model.TrafficRecord{NodeID: 1, ProtocolEndpointID: 3, ReportID: "rollback", Nonce: "rollback", At: time.Now().UTC(), UsedBytes: 5}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if err := EnqueueUsageProjections(tx, []model.TrafficRecord{record}); err != nil {
			return err
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}
	projectionCounts(t, db, 0, 0, 0)
	if err := EnqueueUsageProjections(db, []model.TrafficRecord{{ProtocolEndpointID: 3}}); err == nil {
		t.Fatal("accepted an unpersisted fact")
	}
}
