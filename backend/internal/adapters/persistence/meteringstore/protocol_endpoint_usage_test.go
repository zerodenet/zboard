package meteringstore

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type endpointUsageSQLCapture struct {
	logger.Interface
	statement string
}

func (c *endpointUsageSQLCapture) Trace(ctx context.Context, begin time.Time, query func() (string, int64), err error) {
	c.statement, _ = query()
}

func TestEndpointUsageProjectionAggregatesByUTCDayAndRollsBack(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "endpoint-usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	records := []model.TrafficRecord{
		{ProtocolEndpointID: 3, At: time.Date(2026, 9, 17, 1, 0, 0, 0, time.UTC), RawBytes: 11, UploadBytes: 4, DownloadBytes: 7, UsedBytes: 13},
		{ProtocolEndpointID: 3, At: time.Date(2026, 9, 17, 23, 0, 0, 0, time.UTC), RawBytes: 17, UploadBytes: 5, DownloadBytes: 12, UsedBytes: 19},
		{ProtocolEndpointID: 3, At: time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC), RawBytes: 23, UploadBytes: 8, DownloadBytes: 15, UsedBytes: 29},
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return AddProtocolEndpointUsage(tx, records) }); err != nil {
		t.Fatal(err)
	}
	var rows []model.ProtocolEndpointUsageDaily
	if err := db.Order("usage_date").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].UsedBytes != 32 || rows[0].RecordCount != 2 || rows[1].UsedBytes != 29 || rows[1].RecordCount != 1 {
		t.Fatalf("daily projection = %+v", rows)
	}
	wantRollback := errors.New("rollback")
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := AddProtocolEndpointUsage(tx, records[:1]); err != nil {
			return err
		}
		return wantRollback
	})
	if !errors.Is(err, wantRollback) {
		t.Fatal(err)
	}
	var unchanged model.ProtocolEndpointUsageDaily
	if err := db.First(&unchanged, "protocol_endpoint_id = ? AND usage_date = ?", 3, "2026-09-17").Error; err != nil || unchanged.UsedBytes != 32 {
		t.Fatal(unchanged, err)
	}
}

func TestEndpointUsageProjectionGeneratesMySQLAtomicUpsert(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "endpoint-usage-mysql-sql.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	capture := &endpointUsageSQLCapture{Interface: logger.Discard}
	dryRun, err := gorm.Open(mysql.New(mysql.Config{Conn: db.ConnPool, SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, Logger: capture})
	if err != nil {
		t.Fatal(err)
	}
	err = AddProtocolEndpointUsage(dryRun, []model.TrafficRecord{{ProtocolEndpointID: 2, At: time.Date(2026, 9, 18, 1, 0, 0, 0, time.UTC), UsedBytes: 17}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(capture.statement, "ON DUPLICATE KEY UPDATE") || !strings.Contains(capture.statement, "used_bytes + 17") {
		t.Fatalf("MySQL projection write is not an atomic increment: %s", capture.statement)
	}
}
