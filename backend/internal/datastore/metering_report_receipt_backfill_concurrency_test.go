package datastore_test

import (
	"context"
	"encoding/json"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"testing"
	"time"
)

// Use genuinely separate physical connection pools. SQLite's ordinary one-pool
// serialization must not masquerade as cross-process duplicate protection.
func reportReceiptSecondConnection(t *testing.T, db *gorm.DB) *gorm.DB {
	t.Helper()
	driver, source := datastore.DriverSQLite, ""
	if datastore.IsSQLite(db) {
		var paths []struct {
			Name string
			File string
		}
		if err := db.Raw("PRAGMA database_list").Scan(&paths).Error; err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			if path.Name == "main" {
				source = path.File
			}
		}
	} else {
		driver = datastore.DriverMySQL
		dialect, ok := db.Dialector.(*gormmysql.Dialector)
		if !ok {
			t.Fatal("actual MySQL connection required")
		}
		source = dialect.Config.DSN
	}
	if source == "" {
		t.Fatal("owned fixture connection unavailable")
	}
	other, err := datastore.OpenWithDriver(driver, source, datastore.PoolConfig{MaxOpenConnections: 1, MaxIdleConnections: 1})
	if err != nil {
		t.Fatal("second owned fixture connection failed")
	}
	pool, err := other.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return other
}

func assertReportReceiptConcurrentRange(t *testing.T, db *gorm.DB, after uint, last model.TrafficRecord) {
	t.Helper()
	var before model.TrafficRecord
	if err := db.First(&before, last.ID).Error; err != nil {
		t.Fatal(err)
	}
	baseline, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	other := reportReceiptSecondConnection(t, db)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	type outcome struct {
		row metering.ReportReceiptBackfillResult
		err error
	}
	ready := make(chan struct{})
	done := make(chan outcome, 2)
	for _, connection := range []*gorm.DB{db, other} {
		go func(connection *gorm.DB) {
			<-ready
			row, err := meteringstore.BackfillReportReceipts(ctx, connection, after, last.ID, 1)
			done <- outcome{row, err}
		}(connection)
	}
	close(ready)
	results := []outcome{<-done, <-done}
	for _, result := range results {
		if result.err != nil || result.row.Scanned != 1 || result.row.IdentitiesVerified != 1 || result.row.NextAfterID != last.ID || !result.row.RangeExhausted {
			t.Fatal("concurrent identity registration failed", result.row, result.err)
		}
	}
	if results[0].row != results[1].row {
		t.Fatal("same finite range returned different committed identities")
	}
	var receipts []model.MeteringReportReceipt
	if err := db.Where("node_id = ? AND report_id = ?", last.NodeID, last.ReportID).Find(&receipts).Error; err != nil || len(receipts) != 1 || receipts[0].TrafficRecordID != last.ID || receipts[0].ProtocolEndpointID != last.ProtocolEndpointID {
		t.Fatal("concurrent registration duplicated or changed identity", err)
	}
	var current model.TrafficRecord
	if err := db.First(&current, last.ID).Error; err != nil {
		t.Fatal(err)
	}
	observed, err := json.Marshal(current)
	if err != nil || string(observed) != string(baseline) || current.Nonce != before.Nonce || current.EntitlementEpoch != before.EntitlementEpoch {
		t.Fatal("concurrent registration changed original evidence", err)
	}
}
