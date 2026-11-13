package datastore_test

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestSQLiteMeteringCopyKeepsRetiredLedgerIDsReserved(t *testing.T) {
	assertRetiredAllocatorCopy(t, false)
}
func TestMySQLMeteringCopyKeepsRetiredLedgerIDsReserved(t *testing.T) {
	assertRetiredAllocatorCopy(t, true)
}
func assertRetiredAllocatorCopy(t *testing.T, mysql bool) {
	source := diagnosticSchemaDB(t, mysql)
	row := diagnosticSchemaSource(t, source, false)
	now := time.Now().UTC().Truncate(time.Millisecond)
	old := now.Add(-400 * 24 * time.Hour)
	if e := source.Model(&row).UpdateColumns(map[string]any{"created_at": old, "updated_at": old, "record_at": old, "meta": ""}).Error; e != nil {
		t.Fatal(e)
	}
	if e := source.First(&row, row.ID).Error; e != nil {
		t.Fatal(e)
	}
	if e := source.Transaction(func(tx *gorm.DB) error { return meteringstore.RecordUsageProjections(tx, []model.TrafficRecord{row}) }); e != nil {
		t.Fatal(e)
	}
	if _, e := (meteringstore.UsageProjectionWorker{DB: source}).Drain(t.Context(), 100); e != nil {
		t.Fatal(e)
	}
	worker := meteringstore.LedgerRetention{DB: source, Now: func() time.Time { return now }}
	if n, e := worker.Drain(t.Context(), 100); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	now = now.Add(2 * time.Hour)
	if _, e := worker.Drain(t.Context(), 100); e != nil {
		t.Fatal(e)
	}
	var count int64
	source.Model(&model.TrafficRecord{}).Count(&count)
	if count != 0 {
		t.Fatal("detail did not expire", count)
	}
	target := diagnosticSchemaDB(t, mysql)
	principalRuntimeCopy(t, source, target)
	probe := model.TrafficRecord{NodeID: row.NodeID, UserID: row.UserID, ProtocolEndpointID: row.ProtocolEndpointID, ReportID: "after-copy", Nonce: "after-copy", At: time.Now().UTC()}
	if e := target.Create(&probe).Error; e != nil {
		t.Fatal(e)
	}
	if probe.ID <= row.ID {
		t.Fatal("retired identity was reused by a new ledger row", probe.ID, row.ID)
	}
	var used int64
	if e := target.Model(&model.MeteringSettlementResult{}).Select("COALESCE(SUM(used_bytes),0)").Scan(&used).Error; e != nil || used != 5 {
		t.Fatal("copy lost settlement result", used, e)
	}
}
