package datastore

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestSQLiteUsageProjectionSchemaPreservesInterruptedQueueAndRefusesLostSource(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "projection-schema.db"))
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
	record := model.TrafficRecord{ID: 1, NodeID: 1, ProtocolEndpointID: 7, ReportID: "kept", Nonce: "kept", At: time.Now().UTC(), UsedBytes: 13}
	if err := db.Omit("is_aggregate", "record_count").Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	pending := model.MeteringUsageProjectionPending{TrafficRecordID: 1, ProtocolEndpointID: 7, CreatedAt: time.Now().UTC()}
	if err := db.Omit("HourlyPending", "UserID", "SubscriptionID", "NodeID", "RecordAt", "DeltaRawBytes", "DeltaUploadBytes", "DeltaDownloadBytes", "DeltaUsedBytes", "DeltaRecordCount").Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	// Simulate MySQL-style interruption after DDL but before the version mark.
	if err := db.Where("version = ?", usageProjectionSchemaVersion).Delete(&schemaMigration{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP INDEX idx_usage_projection_endpoint").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := reconcileUsageProjectionSchema(db); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := db.Model(&model.MeteringUsageProjectionPending{}).Where("traffic_record_id = 1 AND protocol_endpoint_id = 7").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("interrupted migration lost intent: %d %v", count, err)
	}
	if !db.Migrator().HasIndex(usageProjectionTable, "idx_usage_projection_endpoint") {
		t.Fatal("interrupted index not recovered")
	}
	if err := db.Delete(&record).Error; err == nil {
		t.Fatal("pending source fact can be deleted")
	}
	if err := db.Migrator().DropTable(usageProjectionTable); err != nil {
		t.Fatal(err)
	}
	if err := preflightUsageProjectionSchema(db); err == nil || !strings.Contains(err.Error(), "restore before startup") {
		t.Fatalf("lost reliable queue accepted: %v", err)
	}
	if err := reconcileUsageProjectionSchema(db); err == nil {
		t.Fatal("lost reliable queue silently recreated")
	}
	if db.Migrator().HasTable(usageProjectionTable) {
		t.Fatal("refusal mutated lost queue state")
	}
}

func TestSQLiteUsageProjectionSchemaRefusesMissingRecoveryColumnOrSourceProtection(t *testing.T) {
	for _, missing := range []string{"column", "foreign_key"} {
		t.Run(missing, func(t *testing.T) {
			db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "invalid-projection.db"))
			if err != nil {
				t.Fatal(err)
			}
			pool, _ := db.DB()
			t.Cleanup(func() { _ = pool.Close() })
			if err := db.AutoMigrate(&schemaMigration{}); err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("CREATE TABLE traffic_records(id integer PRIMARY KEY)").Error; err != nil {
				t.Fatal(err)
			}
			if missing == "column" {
				if err := reconcileUsageProjectionSchema(db); err != nil {
					t.Fatal(err)
				}
				if err := db.Exec("ALTER TABLE metering_usage_projection_pending DROP COLUMN created_at").Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := db.Exec("CREATE TABLE metering_usage_projection_pending(traffic_record_id integer PRIMARY KEY, protocol_endpoint_id integer NOT NULL, created_at datetime NOT NULL)").Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&schemaMigration{Version: usageProjectionSchemaVersion, AppliedAt: time.Now().UTC()}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := reconcileUsageProjectionSchema(db); err == nil || !strings.Contains(err.Error(), "restore") {
				t.Fatalf("missing %s silently accepted/recreated: %v", missing, err)
			}
		})
	}
}
