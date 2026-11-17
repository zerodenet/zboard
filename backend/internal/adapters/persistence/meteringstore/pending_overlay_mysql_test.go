package meteringstore

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// This is a narrow read-SQL fixture, not evidence of migration preparation.
// It owns three minimal tables in a disposable schema on the opt-in test server.
func TestMySQLPendingUsageOverlayKeepsHistoricalAndCurrentSnapshot(t *testing.T) {
	dsn := os.Getenv("ZBOARD_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set isolated ZBOARD_TEST_MYSQL_DSN")
	}
	cfg, err := mysqlconfig.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("zboard_read_overlay_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + name + "`"); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP DATABASE `" + name + "`")
	cfg.DBName = name
	db, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	pool.SetMaxOpenConns(4)
	dimensions := "user_id bigint unsigned NOT NULL,subscription_id bigint unsigned NOT NULL,node_id bigint unsigned NOT NULL,protocol_endpoint_id bigint unsigned NOT NULL,protocol_multiplier_milli bigint NOT NULL"
	amounts := "raw_bytes bigint NOT NULL,upload_bytes bigint NOT NULL DEFAULT 0,download_bytes bigint NOT NULL DEFAULT 0,used_bytes bigint NOT NULL,record_count bigint NOT NULL"
	for _, ddl := range []string{
		"CREATE TABLE traffic_records(id bigint unsigned PRIMARY KEY,record_at datetime(3) NOT NULL,is_aggregate boolean NOT NULL," + dimensions + "," + amounts + ")",
		"CREATE TABLE traffic_usage_hourly(record_at datetime(3) NOT NULL," + dimensions + "," + amounts + ")",
		"CREATE TABLE metering_usage_projection_pending(traffic_record_id bigint unsigned PRIMARY KEY,protocol_endpoint_id bigint unsigned NOT NULL,user_id bigint unsigned NOT NULL,subscription_id bigint unsigned NOT NULL,node_id bigint unsigned NOT NULL,created_at datetime(3) NOT NULL,hourly_pending boolean NOT NULL,record_at datetime(3) NOT NULL,delta_raw_bytes bigint NOT NULL DEFAULT 0,delta_upload_bytes bigint NOT NULL DEFAULT 0,delta_download_bytes bigint NOT NULL DEFAULT 0,delta_used_bytes bigint NOT NULL DEFAULT 0,delta_record_count bigint NOT NULL DEFAULT 0)",
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Hour)
	for i, spec := range []struct {
		at                                  time.Time
		total, count, pending, pendingCount int64
		aggregate                           bool
	}{
		{now.Add(-time.Hour), 70, 7, 20, 2, true}, {now, 30, 3, 10, 1, true}, {now.Add(-2 * time.Hour), 5, 1, 5, 1, false},
	} {
		at := spec.at.Add(10 * time.Minute)
		if err := db.Exec("INSERT INTO traffic_records(id,record_at,is_aggregate,user_id,subscription_id,node_id,protocol_endpoint_id,protocol_multiplier_milli,raw_bytes,used_bytes,record_count) VALUES (?,?,?,7,9,3,11,1000,?,?,?)", i+1, at, spec.aggregate, spec.total, spec.total, spec.count).Error; err != nil {
			t.Fatal(err)
		}
		if spec.aggregate {
			if err := db.Exec("INSERT INTO traffic_usage_hourly(record_at,user_id,subscription_id,node_id,protocol_endpoint_id,protocol_multiplier_milli,raw_bytes,used_bytes,record_count) VALUES (?,7,9,3,11,1000,?,?,?)", spec.at, spec.total-spec.pending, spec.total-spec.pending, spec.count-spec.pendingCount).Error; err != nil {
				t.Fatal(err)
			}
		}
		delta, count := spec.pending, spec.pendingCount
		if !spec.aggregate {
			delta, count = 0, 0
		}
		if err := db.Exec("INSERT INTO metering_usage_projection_pending(traffic_record_id,protocol_endpoint_id,user_id,subscription_id,node_id,created_at,hourly_pending,record_at,delta_raw_bytes,delta_used_bytes,delta_record_count) VALUES (?,11,7,9,3,?,1,?,?,?,?)", i+1, now, spec.at, delta, delta, count).Error; err != nil {
			t.Fatal(err)
		}
	}
	scope := TrafficScope{UserID: 7}
	verify := func(tx *gorm.DB) {
		t.Helper()
		if err := EnsureHourlyUsageReadable(tx, scope, time.Time{}, time.Time{}); err != nil {
			t.Fatal(err)
		}
		var out struct{ Used, Count int64 }
		if err := TrafficAggregateSource(tx, time.Time{}, time.Time{}, scope).Select("SUM(used_bytes) AS used,SUM(record_count) AS count").Scan(&out).Error; err != nil || out.Used != 105 || out.Count != 11 {
			t.Fatal(out, err)
		}
	}
	if err := db.Exec("UPDATE metering_usage_projection_pending SET delta_used_bytes=31 WHERE traffic_record_id=2").Error; err != nil {
		t.Fatal(err)
	}
	if err := EnsureHourlyUsageReadable(db, scope, time.Time{}, time.Time{}); !errors.Is(err, metering.ErrUsageProjectionPending) {
		t.Fatal("invalid delta appeared complete", err)
	}
	if err := db.Exec("UPDATE metering_usage_projection_pending SET delta_used_bytes=10 WHERE traffic_record_id=2").Error; err != nil {
		t.Fatal(err)
	}
	err = db.Transaction(func(read *gorm.DB) error {
		verify(read)
		if err := db.Transaction(func(write *gorm.DB) error {
			if err := write.Exec("UPDATE traffic_usage_hourly SET raw_bytes=raw_bytes+CASE WHEN record_at=? THEN 20 ELSE 10 END,used_bytes=used_bytes+CASE WHEN record_at=? THEN 20 ELSE 10 END,record_count=record_count+CASE WHEN record_at=? THEN 2 ELSE 1 END", now.Add(-time.Hour), now.Add(-time.Hour), now.Add(-time.Hour)).Error; err != nil {
				return err
			}
			if err := write.Exec("INSERT INTO traffic_usage_hourly(record_at,user_id,subscription_id,node_id,protocol_endpoint_id,protocol_multiplier_milli,raw_bytes,used_bytes,record_count) VALUES (?,7,9,3,11,1000,5,5,1)", now.Add(-2*time.Hour)).Error; err != nil {
				return err
			}
			return write.Exec("DELETE FROM metering_usage_projection_pending").Error
		}); err != nil {
			return err
		}
		verify(read) // A concurrent projector commit cannot mix two snapshots.
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	verify(db) // Confirmed totals after projection are identical.
}
