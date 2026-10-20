package datastore

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestSQLiteHourlyProjectionCutoverPreservesHistoricalAndPendingBoundaries(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "hourly-cutover.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&schemaMigration{}, &model.TrafficRecord{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE traffic_records ADD COLUMN entitlement_epoch bigint NOT NULL DEFAULT 0").Error; err != nil {
		t.Fatal(err)
	}
	if err := reconcileUsageProjectionSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := reconcileSQLiteTrafficHourly(db); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)
	old := model.TrafficRecord{ID: 1, NodeID: 3, UserID: 7, ProtocolEndpointID: 9, ReportID: "old", Nonce: "old", At: at, RawBytes: 7, UsedBytes: 11}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table(usageProjectionTable).Create(map[string]any{"traffic_record_id": old.ID, "protocol_endpoint_id": old.ProtocolEndpointID, "created_at": at}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := reconcileHourlyProjectionSchema(db); err != nil {
			t.Fatal(err)
		}
	}
	var legacy model.MeteringUsageProjectionPending
	if err := db.First(&legacy, "traffic_record_id = ?", old.ID).Error; err != nil || legacy.HourlyPending {
		t.Fatal("old hourly completion evidence lost", legacy, err)
	}
	assert := func(want int64) {
		t.Helper()
		var used int64
		if err := db.Table("traffic_usage_hourly").Select("COALESCE(SUM(used_bytes),0)").Scan(&used).Error; err != nil || used != want {
			t.Fatal(used, want, err)
		}
	}
	assert(11)
	var triggers int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name LIKE 'traffic_hourly_%'").Scan(&triggers).Error; err != nil || triggers != 0 {
		t.Fatal(triggers, err)
	}
	current := old
	current.ID = 2
	current.ReportID = "new"
	current.Nonce = "new"
	current.UsedBytes = 13
	if err := db.Create(&current).Error; err != nil {
		t.Fatal(err)
	}
	pending := model.MeteringUsageProjectionPending{TrafficRecordID: current.ID, ProtocolEndpointID: current.ProtocolEndpointID, CreatedAt: at, HourlyPending: true, UserID: current.UserID, NodeID: current.NodeID, RecordAt: &at}
	if err := db.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	assert(11)
	for i := 0; i < 2; i++ {
		if err := db.Transaction(RebuildTrafficHourly); err != nil {
			t.Fatal(err)
		}
		assert(11)
	}
	// Both a lost derived table and a lost older marker must preserve the exact
	// worker-owned boundary instead of rebuilding the new delta twice.
	if err := db.Migrator().DropTable("traffic_usage_hourly"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", trafficHourlyVersion).Error; err != nil {
		t.Fatal(err)
	}
	if err := reconcileSQLiteTrafficHourly(db); err != nil {
		t.Fatal(err)
	}
	if err := reconcileHourlyProjectionSchema(db); err != nil {
		t.Fatal(err)
	}
	assert(11)
	var queue int64
	if err := db.Table(usageProjectionTable).Count(&queue).Error; err != nil || queue != 2 {
		t.Fatal(queue, err)
	}
	if err := PrepareHourlyProjectionRollback(db); err == nil {
		t.Fatal("rollback restored competing writer while pending")
	}
	// Preserve lost recovery-column evidence; do not default it to false and
	// incorrectly label an unprojected fact as already included.
	if err := db.Exec("DROP INDEX idx_usage_hourly_pending_time").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP INDEX idx_usage_hourly_pending_user").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP INDEX idx_usage_hourly_pending_subscription").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP INDEX idx_usage_hourly_pending_node").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP INDEX idx_usage_hourly_pending_endpoint").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE metering_usage_projection_pending DROP COLUMN hourly_pending").Error; err != nil {
		t.Fatal(err)
	}
	if err := preflightHourlyProjectionSchema(db); err == nil || !strings.Contains(err.Error(), "restore") {
		t.Fatal("lost completion coverage accepted", err)
	}
}

// A small reproducible storage sample, not a capacity or write-throughput test.
// Run with -run TestSQLiteHourlyProjectionPendingStorageSample -v -count=1.
func TestSQLiteHourlyProjectionPendingStorageSample(t *testing.T) {
	for _, current := range []bool{false, true} {
		t.Run(fmt.Sprint(current), func(t *testing.T) {
			db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "pending-storage.db"))
			if err != nil {
				t.Fatal(err)
			}
			pool, _ := db.DB()
			t.Cleanup(func() { _ = pool.Close() })
			if err := db.AutoMigrate(&schemaMigration{}, &model.TrafficRecord{}); err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("ALTER TABLE traffic_records ADD COLUMN entitlement_epoch bigint NOT NULL DEFAULT 0").Error; err != nil {
				t.Fatal(err)
			}
			if err := reconcileUsageProjectionSchema(db); err != nil {
				t.Fatal(err)
			}
			if current {
				if err := reconcileHourlyProjectionSchema(db); err != nil {
					t.Fatal(err)
				}
			}
			at := time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)
			records := make([]model.TrafficRecord, 1000)
			for i := range records {
				key := fmt.Sprint(i)
				records[i] = model.TrafficRecord{UserID: uint(i%64 + 1), SubscriptionID: uint(i%128 + 1), NodeID: uint(i%16 + 1), ProtocolEndpointID: uint(i%8 + 1), ReportID: key, Nonce: key, At: at.Add(time.Duration(i) * time.Second), UsedBytes: 11}
			}
			if err := db.CreateInBatches(&records, 100).Error; err != nil {
				t.Fatal(err)
			}
			if current {
				pending := make([]model.MeteringUsageProjectionPending, 0, len(records))
				for _, record := range records {
					hour := record.At.UTC().Truncate(time.Hour)
					pending = append(pending, model.MeteringUsageProjectionPending{TrafficRecordID: record.ID, ProtocolEndpointID: record.ProtocolEndpointID, CreatedAt: record.CreatedAt, HourlyPending: true, UserID: record.UserID, SubscriptionID: record.SubscriptionID, NodeID: record.NodeID, RecordAt: &hour})
				}
				if err := db.CreateInBatches(&pending, 100).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := db.Exec("INSERT INTO metering_usage_projection_pending(traffic_record_id,protocol_endpoint_id,created_at) SELECT id,protocol_endpoint_id,created_at FROM traffic_records").Error; err != nil {
					t.Fatal(err)
				}
			}
			var rows []struct {
				Name  string
				Bytes int64
			}
			if err := db.Raw("SELECT name,SUM(pgsize) AS bytes FROM dbstat WHERE name = 'metering_usage_projection_pending' OR name LIKE 'idx_usage_projection_%' OR name LIKE 'idx_usage_hourly_pending_%' GROUP BY name ORDER BY name").Scan(&rows).Error; err != nil {
				t.Fatalf("cannot measure pending storage: %v", err)
			}
			var total int64
			for _, row := range rows {
				total += row.Bytes
				t.Logf("0054=%t object=%s bytes=%d", current, row.Name, row.Bytes)
			}
			t.Logf("0054=%t pending_facts=1000 users=64 subscriptions=128 nodes=16 endpoints=8 object_pages_bytes=%d allocated_bytes_per_pending_fact=%.3f", current, total, float64(total)/1000)
		})
	}
}

func TestSQLiteHourlyProjectionRollbackRestoresLegacyWriterOnlyAfterDrain(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "hourly-rollback.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := PrepareHourlyProjectionRollback(db); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)
	record := model.TrafficRecord{NodeID: 1, ReportID: "old-writer", Nonce: "old-writer", At: at, UsedBytes: 11}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	var used int64
	if err := db.Table("traffic_usage_hourly").Select("SUM(used_bytes)").Scan(&used).Error; err != nil || used != 11 {
		t.Fatal(used, err)
	}
	// Re-upgrading drops only the legacy writer. It does not replay an already
	// accounted historical bucket, even though 0054 remains in migration history.
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	record.ID = 0
	record.ReportID = "new-writer"
	record.Nonce = "new-writer"
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("traffic_usage_hourly").Select("SUM(used_bytes)").Scan(&used).Error; err != nil || used != 11 {
		t.Fatal("two writers active after re-upgrade", used, err)
	}
}

func TestSQLiteHourlyProjectionRollbackRefusesIncompleteConfirmedState(t *testing.T) {
	for _, state := range []string{"missing-version", "missing-hourly", "empty-hourly"} {
		t.Run(state, func(t *testing.T) {
			db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "rollback-incomplete.db"))
			if err != nil {
				t.Fatal(err)
			}
			pool, _ := db.DB()
			t.Cleanup(func() { _ = pool.Close() })
			if err := RunMigrations(db); err != nil {
				t.Fatal(err)
			}
			switch state {
			case "missing-version":
				err = db.Exec("DELETE FROM schema_migrations WHERE version = ?", hourlyProjectionSchemaVersion).Error
			case "missing-hourly":
				err = db.Migrator().DropTable("traffic_usage_hourly")
			case "empty-hourly":
				// A confirmed historical fact without its derived bucket must be
				// recovered before reverting to a synchronous writer.
				err = db.Create(&model.TrafficRecord{NodeID: 1, ReportID: "confirmed", Nonce: "confirmed", At: time.Now().UTC(), UsedBytes: 11}).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := PrepareHourlyProjectionRollback(db); err == nil {
				t.Fatal("rollback accepted incomplete confirmed state")
			}
			var triggers int64
			if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name LIKE 'traffic_hourly_%'").Scan(&triggers).Error; err != nil || triggers != 0 {
				t.Fatal("rejected rollback changed writer ownership", triggers, err)
			}
		})
	}
}
