package platformstore

import (
	"context"
	"fmt"
	"gorm.io/driver/mysql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

func historyDatabase(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, path)
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	return db
}
func TestMetricHistoryPersistsAcrossDatabaseReopenAndKeepsZeroUnknown(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "monitor.db")
	now := time.Now().UTC().Truncate(time.Minute)
	db := historyDatabase(t, path)
	store := SystemMetricHistory{DB: db}
	row := platform.SystemMinute{At: now, Sums: [5]float64{0, 40}, Counts: [5]int{1, 2}}
	if err := store.Save(ctx, []platform.SystemMinute{row}, now); err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	pool.Close()
	reopened := historyDatabase(t, path)
	rows, err := (SystemMetricHistory{DB: reopened}).Load(ctx, now)
	if err != nil || len(rows) != 1 {
		t.Fatalf("reopen: %v %+v", err, rows)
	}
	point := rows[0].Point()
	if point.CPU == nil || *point.CPU != 0 || point.Disk != nil || *point.Memory != 20 {
		t.Fatal("zero/unknown/weights changed")
	}
}
func TestMetricHistoryFixedSlotsAndTimeExpiry(t *testing.T) {
	ctx := context.Background()
	db := historyDatabase(t, filepath.Join(t.TempDir(), "ring.db"))
	store := SystemMetricHistory{DB: db}
	start := time.Now().UTC().Truncate(time.Minute)
	rows := make([]platform.SystemMinute, 1440)
	for i := range rows {
		rows[i] = platform.SystemMinute{At: start.Add(time.Duration(i) * time.Minute)}
	}
	end := rows[1439].At
	if err := store.Save(ctx, rows, end); err != nil {
		t.Fatal(err)
	}
	// At exactly 24h the oldest bucket is still in the query window, but its
	// physical slot is atomically reused. Repeated startup never adds a sequence.
	for i := 0; i < 3; i++ {
		if err := store.Save(ctx, []platform.SystemMinute{{At: start.Add(24 * time.Hour)}}, start.Add(24*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	db.Model(&metricMinute{}).Count(&count)
	if count != 1440 {
		t.Fatalf("rows=%d", count)
	}
	if err := db.Exec("INSERT INTO runtime_metric_minutes (slot,minute_at,sample_count,cpu_sum,memory_sum,disk_sum,rx_sum,tx_sum,cpu_count,memory_count,disk_count,rx_count,tx_count) VALUES (1440,?,0,0,0,0,0,0,0,0,0,0,0)", end).Error; err == nil {
		t.Fatal("schema accepted a slot beyond the hard cap")
	}
	loaded, err := store.Load(ctx, start.Add(49*time.Hour))
	if err != nil || len(loaded) != 0 {
		t.Fatal("old history returned", err)
	}
	db.Model(&metricMinute{}).Count(&count)
	if count != 0 {
		t.Fatal("expired rows not physically removed")
	}
}
func TestMetricHistoryWriteFailureIsAtomic(t *testing.T) {
	ctx := context.Background()
	db := historyDatabase(t, filepath.Join(t.TempDir(), "atomic.db"))
	store := SystemMetricHistory{DB: db}
	now := time.Now().UTC().Truncate(time.Minute)
	first := platform.SystemMinute{At: now, Sums: [5]float64{10}, Counts: [5]int{1}}
	if err := store.Save(ctx, []platform.SystemMinute{first}, now); err != nil {
		t.Fatal(err)
	}
	second := now.Add(time.Minute)
	if err := db.Exec(fmt.Sprintf("CREATE TRIGGER reject_metric BEFORE INSERT ON runtime_metric_minutes WHEN NEW.slot = %d BEGIN SELECT RAISE(ABORT, 'simulated failure'); END", uint16((second.Unix()/60)%1440))).Error; err != nil {
		t.Fatal(err)
	}
	first.Sums[0] = 99
	if err := store.Save(ctx, []platform.SystemMinute{first, {At: second}}, second); err == nil {
		t.Fatal("expected atomic failure")
	}
	rows, err := store.Load(ctx, second)
	if err != nil || len(rows) != 1 || rows[0].Sums[0] != 10 {
		t.Fatal("partially committed monitor history", err)
	}
}

type restartMetricReader struct {
	at  time.Time
	cpu float64
}

func (r *restartMetricReader) Read() platform.SystemSample {
	return platform.SystemSample{At: r.at, CPU: &r.cpu}
}
func waitMetricSample(t *testing.T, m *platform.RuntimeMonitor, now time.Time) platform.SystemSnapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := m.Snapshot(60, now)
		if s.Current != nil {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("monitor failed to collect after restoration")
	return platform.SystemSnapshot{}
}
func TestMetricHistoryMonitorRestartResumesSameMinute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	now := time.Now().UTC()
	db := historyDatabase(t, path)
	first := platform.NewRuntimeMonitor(&restartMetricReader{at: now, cpu: 20}, SystemMetricHistory{DB: db})
	first.Start()
	t.Cleanup(first.Close)
	waitMetricSample(t, first, now)
	first.Close()
	pool, _ := db.DB()
	pool.Close()
	// A new connection and new monitor have no shared Go cache.
	nextDB := historyDatabase(t, path)
	next := platform.NewRuntimeMonitor(&restartMetricReader{at: now, cpu: 80}, SystemMetricHistory{DB: nextDB})
	next.Start()
	t.Cleanup(next.Close)
	snapshot := waitMetricSample(t, next, now)
	if len(snapshot.Points) != 1 || snapshot.Points[0].CPU == nil || *snapshot.Points[0].CPU != 50 {
		t.Fatalf("restart lost weights/history: %+v", snapshot.Points)
	}
	if snapshot.History.LastSavedAt == nil || snapshot.History.State != "ready" {
		t.Fatal(snapshot.History)
	}
	next.Close()
	saved, err := (SystemMetricHistory{DB: nextDB}).Load(context.Background(), now)
	if err != nil || len(saved) != 1 || saved[0].Counts[0] != 2 {
		t.Fatalf("shutdown checkpoint: %+v %v", saved, err)
	}
}

func TestMetricHistoryStaleRetryCannotRegressSameMinute(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	ctx := context.Background()
	db := historyDatabase(t, filepath.Join(t.TempDir(), "stale.db"))
	store := SystemMetricHistory{DB: db}
	old := platform.SystemMinute{At: now, Sums: [5]float64{10}, Counts: [5]int{1}}
	fresh := platform.SystemMinute{At: now, Sums: [5]float64{50}, Counts: [5]int{2}}
	for _, row := range []platform.SystemMinute{old, fresh, old} {
		if err := store.Save(ctx, []platform.SystemMinute{row}, now); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := store.Load(ctx, now)
	if err != nil || len(saved) != 1 || saved[0].Counts[0] != 2 || saved[0].Sums[0] != 50 {
		t.Fatal("stale retry regressed retained aggregate", err, saved)
	}
}

func TestMetricHistoryMySQLUpsertUsesGuardedOrderedAssignments(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{DSN: "user:unused@tcp(127.0.0.1:1)/test?parseTime=true", SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return tx.Clauses(metricHistoryConflict("mysql")).Create(&metricMinute{Slot: 1, MinuteAt: time.Now().UTC()})
	})
	if !strings.Contains(sql, "ON DUPLICATE KEY UPDATE") || !strings.Contains(sql, "sample_count <= VALUES(sample_count)") || strings.Contains(sql, "excluded.") {
		t.Fatalf("MySQL dialect lost the history fence: %s", sql)
	}
	if strings.LastIndex(sql, "`minute_at`=CASE") < strings.LastIndex(sql, "`sample_count`=CASE") {
		t.Fatal("MySQL timestamp must be updated last")
	}
}
func TestMetricHistoryInterruptedMigrationResumesWithoutDroppingData(t *testing.T) {
	ctx := context.Background()
	db := historyDatabase(t, filepath.Join(t.TempDir(), "resume.db"))
	store := SystemMetricHistory{DB: db}
	now := time.Now().UTC().Truncate(time.Minute)
	if err := store.Save(ctx, []platform.SystemMinute{{At: now}}, now); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version='0082_runtime_metric_history.up.sql'").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP INDEX idx_runtime_metric_expiry").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	rows, err := store.Load(ctx, now)
	if err != nil || len(rows) != 1 {
		t.Fatal("migration recovery discarded retained history", err)
	}
}
