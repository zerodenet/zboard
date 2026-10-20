package datastore_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func hourlyCopyDatabase(t *testing.T, mysql bool) *gorm.DB {
	t.Helper()
	var db *gorm.DB
	if mysql {
		db, _ = hourlyMySQLFixture(t)
	} else {
		var err error
		db, err = datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "hourly-copy.db"))
		if err != nil {
			t.Fatal(err)
		}
		pool, _ := db.DB()
		t.Cleanup(func() { _ = pool.Close() })
	}
	if err := application.PrepareDatabaseSchema(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSQLiteHourlyProjectionCopyPreservesUnprojectedBoundary(t *testing.T) {
	checkHourlyProjectionCopy(t, false, false)
}
func TestMySQLHourlyProjectionCopyPreservesUnprojectedBoundary(t *testing.T) {
	for _, pair := range []struct {
		name           string
		source, target bool
	}{{"sqlite_to_mysql", false, true}, {"mysql_to_sqlite", true, false}, {"mysql_to_mysql", true, true}} {
		t.Run(pair.name, func(t *testing.T) { checkHourlyProjectionCopy(t, pair.source, pair.target) })
	}
}

func checkHourlyProjectionCopy(t *testing.T, sourceMySQL, targetMySQL bool) {
	t.Helper()
	source, target := hourlyCopyDatabase(t, sourceMySQL), hourlyCopyDatabase(t, targetMySQL)
	for _, parent := range []any{
		&model.User{ID: 7, Email: "hourly-copy@example.test", Password: "unused", Status: "active"},
		&model.Node{ID: 3, Name: "copy-node", Address: "node.example.test", Config: "{}"},
		&model.ProtocolEndpoint{ID: 9, NodeID: 3, Name: "copy-endpoint", Protocol: "vless", Address: "node.example.test", Port: 443, PublicPort: 443, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]", MultiplierMilli: 1000},
	} {
		if err := source.Create(parent).Error; err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 10, 20, 1, 15, 0, 0, time.UTC)
	old := model.TrafficRecord{NodeID: 3, UserID: 7, SubscriptionID: 2, ProtocolEndpointID: 9, ReportID: "legacy-confirmed", Nonce: "legacy-confirmed", At: at, RawBytes: 7, UsedBytes: 11}
	if err := source.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	if err := source.Transaction(datastore.RebuildTrafficHourly); err != nil {
		t.Fatal(err)
	}
	if err := source.Create(&model.MeteringUsageProjectionPending{TrafficRecordID: old.ID, ProtocolEndpointID: old.ProtocolEndpointID, CreatedAt: at}).Error; err != nil {
		t.Fatal(err)
	}
	current := old
	current.ID = 0
	current.ReportID = "new-pending"
	current.Nonce = "new-pending"
	current.UsedBytes = 13
	if err := source.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&current).Error; err != nil {
			return err
		}
		return meteringstore.RecordUsageProjections(tx, []model.TrafficRecord{current})
	}); err != nil {
		t.Fatal(err)
	}
	if err := datastore.WithCopyDestination(context.Background(), target, func(tx *gorm.DB) error {
		tables, err := datastore.MigrationTables(tx)
		if err != nil {
			return err
		}
		if err := datastore.ClearCopyDestinationRows(tx, tables); err != nil {
			return err
		}
		return datastore.CopyApplicationData(source, tx)
	}); err != nil {
		t.Fatal(err)
	}
	assert := func(db *gorm.DB, wantUsed, wantPending int64) {
		t.Helper()
		var used, pending int64
		if err := db.Table("traffic_usage_hourly").Select("SUM(used_bytes)").Scan(&used).Error; err != nil || used != wantUsed {
			t.Fatal(used, wantUsed, err)
		}
		if err := db.Model(&model.MeteringUsageProjectionPending{}).Count(&pending).Error; err != nil || pending != wantPending {
			t.Fatal(pending, wantPending, err)
		}
	}
	assert(source, 11, 2)
	assert(target, 11, 2)
	if count, err := (meteringstore.UsageProjectionWorker{DB: target}).Drain(context.Background(), 200); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	assert(target, 24, 0)
	assert(source, 11, 2)
	var daily int64
	if err := target.Model(&model.ProtocolEndpointUsageDaily{}).Select("SUM(used_bytes)").Scan(&daily).Error; err != nil || daily != 24 {
		t.Fatal(daily, err)
	}
}
