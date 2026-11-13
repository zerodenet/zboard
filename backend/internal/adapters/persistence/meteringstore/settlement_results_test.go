package meteringstore

import (
	"path/filepath"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func resultTotal(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var sum int64
	if err := settlementResultSource(db).Select("COALESCE(SUM(used_bytes),0)").Scan(&sum).Error; err != nil {
		t.Fatal(err)
	}
	return sum
}
func TestSettlementResultsTransferMutablePrefixAndNewContributions(t *testing.T) {
	db := usageProjectionDB(t, filepath.Join(t.TempDir(), "results.db"))
	first := model.TrafficRecord{NodeID: 1, UserID: 2, SubscriptionID: 3, ReportID: "first", Nonce: "first", RawBytes: 3, UsedBytes: 5, RecordCount: 1, ProtocolMultiplierMilli: 1500, IsAggregate: true}
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	// Simulate a pre-upgrade captured source. Its subsequent canonical increment
	// must remain in the old source until this prefix is transferred.
	if err := db.Model(&model.MeteringSettlementProgress{}).Where("id=1").Update("upper_id", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	first.RawBytes, first.UsedBytes = 6, 10
	if err := db.Model(&first).Select("raw_bytes", "used_bytes").Updates(&first).Error; err != nil {
		t.Fatal(err)
	}
	delta := first
	delta.RawBytes, delta.UsedBytes = 3, 5
	if err := db.Transaction(func(tx *gorm.DB) error { return addSettlementResults(tx, []model.TrafficRecord{delta}, false) }); err != nil {
		t.Fatal(err)
	}
	if got := resultTotal(t, db); got != 10 {
		t.Fatal(got)
	}
	store := SettlementResults{DB: db}
	if n, err := store.Drain(t.Context(), 10); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if got := resultTotal(t, db); got != 10 {
		t.Fatal("transfer doubled result", got)
	}
	if n, err := store.Drain(t.Context(), 10); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	// A later increment to the same covered minute contributes only its delta.
	if err := db.Model(&first).Updates(map[string]any{"raw_bytes": 9, "used_bytes": 15}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return addSettlementResults(tx, []model.TrafficRecord{delta}, false) }); err != nil {
		t.Fatal(err)
	}
	if got := resultTotal(t, db); got != 15 {
		t.Fatal("covered mutable source miscounted", got)
	}
	second := first
	second.ID = 0
	second.ResultRecorded = false
	second.ReportID = "second"
	second.Nonce = "second"
	second.RawBytes = 3
	second.UsedBytes = 5
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&second).Error; err != nil {
			return err
		}
		return addSettlementResults(tx, []model.TrafficRecord{second, second}, false)
	}); err != nil {
		t.Fatal(err)
	}
	if got := resultTotal(t, db); got != 20 {
		t.Fatal("new source repeated within batch", got)
	}
	var count int64
	db.Model(&model.MeteringSettlementResult{}).Count(&count)
	if count != 1 {
		t.Fatal("result grew per report", count)
	}
}
func TestSettlementResultFailureRollsBackCoverageAndTotals(t *testing.T) {
	db := usageProjectionDB(t, filepath.Join(t.TempDir(), "rollback.db"))
	row := model.TrafficRecord{NodeID: 1, ReportID: "rollback", Nonce: "rollback", RawBytes: 1, UsedBytes: 2}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER reject_result_coverage BEFORE UPDATE ON traffic_records WHEN NEW.result_recorded=1 BEGIN SELECT RAISE(ABORT,'coverage failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return addSettlementResults(tx, []model.TrafficRecord{row}, false) }); err == nil {
		t.Fatal("failure accepted")
	}
	var count int64
	db.Model(&model.MeteringSettlementResult{}).Count(&count)
	if count != 0 {
		t.Fatal("result survived failed transaction")
	}
	if got := resultTotal(t, db); got != 2 {
		t.Fatal("failed transfer hid source", got)
	}
}
