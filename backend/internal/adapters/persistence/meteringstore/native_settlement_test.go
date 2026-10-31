package meteringstore

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestNativeMinuteBatchWritesOneLedgerAndOneDeltaWithoutRerounding(t *testing.T) {
	db := trafficAggregateDB(t)
	at := time.Now().UTC().Truncate(time.Minute)
	records := make([]model.TrafficRecord, 1000)
	for i := range records {
		records[i] = model.TrafficRecord{UserID: 7, SubscriptionID: 9, EntitlementEpoch: 1, NodeID: 3, ProtocolEndpointID: 11, ReportID: fmt.Sprint("report-", i), RawBytes: 1, UsedBytes: 2, ProtocolMultiplierMilli: 1001, At: at.Add(time.Duration(i%60) * time.Second), CreatedAt: at.Add(5 * time.Second)}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := storeNativeSettlementBatch(tx, records, map[uint]time.Time{9: at}); err != nil {
			return err
		}
		return RecordUsageProjections(tx, records)
	}); err != nil {
		t.Fatal(err)
	}
	var ledger model.TrafficRecord
	if err := db.First(&ledger).Error; err != nil || ledger.RecordCount != 1000 || ledger.RawBytes != 1000 || ledger.UsedBytes != 2000 || !ledger.IsAggregate {
		t.Fatal(ledger, err)
	}
	for _, table := range []string{"traffic_records", "metering_usage_projection_pending"} {
		var n int64
		if err := db.Table(table).Count(&n).Error; err != nil || n != 1 {
			t.Fatal(table, n, err)
		}
	}
	var p model.MeteringUsageProjectionPending
	if err := db.First(&p).Error; err != nil || p.DeltaUsedBytes != 2000 || p.DeltaRecordCount != 1000 {
		t.Fatal(p, err)
	}
	var receipts int64
	if err := db.Model(&model.MeteringReportReceipt{}).Count(&receipts).Error; err != nil || receipts != 1000 {
		t.Fatal(receipts, err)
	}
	if n, err := (UsageProjectionWorker{DB: db}).Drain(t.Context(), 200); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var total struct{ Used, Count int64 }
	if err := db.Table("traffic_usage_hourly").Select("SUM(used_bytes) AS used,SUM(record_count) AS count").Scan(&total).Error; err != nil || total.Used != 2000 || total.Count != 1000 {
		t.Fatal(total, err)
	}
	t.Log("1000 independently rounded reports -> one minute ledger row, one pending delta, 1000 permanent report identities")
}
func TestNativeMinuteCreatedPartitionsPreserveMillisecondResetBoundary(t *testing.T) {
	db := trafficAggregateDB(t)
	anchor := time.Date(2026, 9, 1, 0, 0, 17, 123456789, time.UTC)
	cut := anchor.AddDate(0, 1, 0)
	for i, used := range []int64{7, 3} {
		created := cut.Add(-time.Nanosecond)
		if i == 1 {
			created = cut.Add(time.Nanosecond)
		}
		record := model.TrafficRecord{UserID: 7, SubscriptionID: 9, EntitlementEpoch: 1, NodeID: 3, ProtocolEndpointID: 11, ReportID: fmt.Sprint("boundary-", i), RawBytes: used, UsedBytes: used, ProtocolMultiplierMilli: 1000, At: cut, CreatedAt: created}
		if err := db.Transaction(func(tx *gorm.DB) error { return storeNativeSettlement(tx, &record, anchor) }); err != nil {
			t.Fatal(err)
		}
	}
	var n int64
	if err := db.Model(&model.TrafficRecord{}).Count(&n).Error; err != nil || n != 2 {
		t.Fatal("opposite billing cycles merged", n, err)
	}
	var since int64
	if err := db.Model(&model.TrafficRecord{}).Select("SUM(used_bytes)").Where("subscription_id = ? AND entitlement_epoch = ? AND created_at >= ?", 9, 1, cut).Scan(&since).Error; err != nil || since != 3 {
		t.Fatal("precise reset carry changed", since, err)
	}
}

func TestMinuteSchemaKeepsPreviouslySealedLegacyFactDigest(t *testing.T) {
	r := model.TrafficRecord{ID: 7, ReportID: "original", Nonce: "nonce", RawBytes: 3, UsedBytes: 5, At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	original := proofFactDigest(r)
	r.RecordCount = 1
	if proofFactDigest(r) != original {
		t.Fatal("implicit legacy report count changed archived proof identity")
	}
	r.IsAggregate = true
	r.RecordCount = 2
	if proofFactDigest(r) == original {
		t.Fatal("canonical count omitted from canonical identity")
	}
}
