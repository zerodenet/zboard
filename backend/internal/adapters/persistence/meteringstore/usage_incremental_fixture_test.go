package meteringstore

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
	"time"
)

type incrementalFixture struct {
	db, readDB *gorm.DB
	cache      *IncrementalCache
}

func newIncrementalFixture(t *testing.T) *incrementalFixture {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	return &incrementalFixture{db: db, readDB: db, cache: &IncrementalCache{}}
}
func (f *incrementalFixture) configureReads() (func() error, error) {
	view, closeView, err := datastore.OpenReadView(f.db)
	if err != nil {
		return nil, err
	}
	f.readDB = view
	return closeView, nil
}
func (f incrementalFixture) seedUsage(t *testing.T) {
	t.Helper()
	// Non-monotonic timestamps and interleaved IDs expose pre-group ID filters.
	for _, row := range []struct {
		id         uint
		stamp      string
		node, user uint
		bytes      int64
	}{
		{1, "01:05:00", 1, 1, 10}, {2, "02:01:00", 1, 1, 20},
		{3, "01:10:00", 2, 1, 30}, {4, "01:55:00", 1, 1, 40},
		{5, "00:01:00", 1, 1, 50}, {6, "01:59:59", 2, 1, 60},
		{7, "02:01:00", 1, 2, 999},
	} {
		at, _ := time.Parse("2006-01-02 15:04:05", "2026-09-01 "+row.stamp)
		record := model.TrafficRecord{ID: row.id, NodeID: row.node, UserID: row.user, SubscriptionID: 1, ProtocolEndpointID: row.id, ReportID: fmt.Sprint(row.id), Nonce: fmt.Sprint(row.id), At: at, ProtocolMultiplierMilli: 1000, RawBytes: row.bytes, UsedBytes: row.bytes, DownloadBytes: row.bytes}
		if err := f.db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
	}
}
