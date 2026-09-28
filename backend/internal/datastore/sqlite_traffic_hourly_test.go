package datastore

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
	"time"
)

func TestTrafficHourlyMaintainsLedgerAcrossMutationsAndRollback(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "hourly.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	assert := func(want int64, count int64) {
		t.Helper()
		var got struct{ Used, Count int64 }
		if err := db.Table("traffic_usage_hourly").Select("COALESCE(SUM(used_bytes),0) AS used, COALESCE(SUM(record_count),0) AS count").Scan(&got).Error; err != nil {
			t.Fatal(err)
		}
		if got.Used != want || got.Count != count {
			t.Fatalf("projection %+v, want bytes %d count %d", got, want, count)
		}
	}
	record := model.TrafficRecord{UserID: 1, NodeID: 2, UsedBytes: 70, At: time.Date(2026, 9, 28, 8, 20, 0, 0, time.UTC)}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	assert(70, 1)
	if err := db.Model(&record).Updates(map[string]interface{}{"used_bytes": 100, "node_id": 3, "record_at": record.At.Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	assert(100, 1)
	_ = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&record).Update("used_bytes", 999).Error; err != nil {
			t.Fatal(err)
		}
		return errors.New("rollback")
	})
	assert(100, 1)
	for _, recursive := range []int{0, 1} {
		statement := "PRAGMA recursive_triggers = OFF"
		if recursive == 1 {
			statement = "PRAGMA recursive_triggers = ON"
		}
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("INSERT OR REPLACE INTO traffic_records(id,user_id,node_id,used_bytes,record_at) VALUES(?,?,?,?,?)", record.ID, 1, 4, 200, record.At.Add(2*time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
		assert(200, 1)
	}
	if err := db.Delete(&record).Error; err != nil {
		t.Fatal(err)
	}
	assert(0, 0)
}

func TestTrafficHourlyBackfillsExistingLedgerOnce(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "backfill.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"traffic_hourly_insert", "traffic_hourly_update", "traffic_hourly_delete", "traffic_hourly_replace"} {
		if err := db.Exec("DROP TRIGGER " + name).Error; err != nil {
			t.Fatal(err)
		}
	}
	db.Exec("DROP TABLE traffic_usage_hourly")
	db.Exec("DELETE FROM schema_migrations WHERE version = '0024_traffic_usage_hourly.up.sql'")
	if err := db.Create(&model.TrafficRecord{UsedBytes: 123, At: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := RunMigrations(db); err != nil {
			t.Fatal(err)
		}
	}
	var total int64
	if err := db.Table("traffic_usage_hourly").Select("SUM(used_bytes)").Scan(&total).Error; err != nil {
		t.Fatal(err)
	}
	if total != 123 {
		t.Fatalf("backfill total %d", total)
	}
}
