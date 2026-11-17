package meteringstore

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestLiveHourReadsMinuteSettlementsWithoutWaitingForProjection(t *testing.T) {
	db := trafficAggregateDB(t)
	now := time.Date(2026, 10, 20, 1, 30, 0, 0, time.UTC)
	db = db.Session(&gorm.Session{NowFunc: func() time.Time { return now }})
	appendReport := func(id string, at time.Time, used int64) {
		t.Helper()
		rows := []model.TrafficRecord{{UserID: 7, SubscriptionID: 9, EntitlementEpoch: 1, NodeID: 3, ProtocolEndpointID: 11, ReportID: id, RawBytes: used, UsedBytes: used, ProtocolMultiplierMilli: 1000, At: at, CreatedAt: now}}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := storeNativeSettlementBatch(tx, rows, map[uint]time.Time{9: now.AddDate(0, -1, 0)}); err != nil {
				return err
			}
			return RecordUsageProjections(tx, rows)
		}); err != nil {
			t.Fatal(err)
		}
	}
	appendReport("closed", now.Add(-75*time.Minute), 7)
	appendReport("live-first", now.Add(-15*time.Minute), 11)
	appendReport("future", now.Add(105*time.Minute), 13)
	worker := UsageProjectionWorker{DB: db}
	if n, err := worker.Drain(context.Background(), 200); err != nil || n != 3 {
		t.Fatal(n, err)
	}
	appendReport("live-pending", now.Add(-15*time.Minute+5*time.Second), 12)
	scope := TrafficScope{UserID: 7}
	windows := []struct {
		from, to    time.Time
		used, count int64
	}{
		{time.Time{}, time.Time{}, 43, 4},
		{now.Truncate(time.Hour), now.Truncate(time.Hour).Add(time.Hour), 23, 2},
		{now.Add(-time.Hour), now, 23, 2},
		{now.Truncate(24 * time.Hour), now.Truncate(24 * time.Hour).Add(24 * time.Hour), 43, 4},
	}
	verify := func() {
		t.Helper()
		for _, w := range windows {
			if err := EnsureHourlyUsageReadable(db, scope, w.from, w.to); err != nil {
				t.Fatal("live hour blocked on projection", err)
			}
			var out struct{ Used, Count int64 }
			if err := TrafficAggregateSource(db, w.from, w.to, scope).Select("COALESCE(SUM(used_bytes),0) AS used,COALESCE(SUM(record_count),0) AS count").Scan(&out).Error; err != nil || out.Used != w.used || out.Count != w.count {
				t.Fatalf("window %v..%v: %+v want %d/%d: %v", w.from, w.to, out, w.used, w.count, err)
			}
		}
	}
	verify()
	if n, err := worker.Drain(context.Background(), 200); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	verify() // A projected live minute must not be counted twice.
	appendReport("closed-pending", now.Add(-70*time.Minute), 17)
	if err := EnsureHourlyUsageReady(db, scope, time.Time{}, time.Time{}); !errors.Is(err, metering.ErrUsageProjectionPending) {
		t.Fatal("closed-hour completeness fence lost", err)
	}
	if err := EnsureHourlyUsageReady(db, TrafficScope{UserID: 8}, time.Time{}, time.Time{}); err != nil {
		t.Fatal("unrelated scope blocked", err)
	}
	if err := EnsureHourlyUsageReadable(db, scope, time.Time{}, time.Time{}); err != nil {
		t.Fatal("historical pending increment blocked bounded read", err)
	}
	var total int64
	if err := TrafficAggregateSource(db, time.Time{}, time.Time{}, scope).Select("SUM(used_bytes)").Scan(&total).Error; err != nil || total != 60 {
		t.Fatal("historical overlay", total, err)
	}
	var ghosts []model.MeteringUsageProjectionPending
	hour := now.Truncate(time.Hour)
	for i := 0; i <= UsageProjectionBatchLimit; i++ {
		ghosts = append(ghosts, model.MeteringUsageProjectionPending{TrafficRecordID: uint(900000 + i), UserID: 8, HourlyPending: true, RecordAt: &hour, CreatedAt: now})
	}
	var sources []model.TrafficRecord
	for _, p := range ghosts {
		sources = append(sources, model.TrafficRecord{ID: p.TrafficRecordID, UserID: 8, At: now, ReportID: fmt.Sprint("ghost-", p.TrafficRecordID), Nonce: fmt.Sprint("ghost-", p.TrafficRecordID)})
	}
	if err := db.CreateInBatches(&sources, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.CreateInBatches(&ghosts, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := EnsureHourlyUsageReadable(db, TrafficScope{UserID: 8}, time.Time{}, time.Time{}); !errors.Is(err, metering.ErrUsageProjectionPending) {
		t.Fatal("oversized pending overlay admitted", err)
	}
	if err := EnsureHourlyUsageReadable(db, scope, time.Time{}, time.Time{}); err != nil {
		t.Fatal("unrelated backlog blocked read", err)
	}
	if err := db.Where("traffic_record_id > ?", 900000).Delete(&model.MeteringUsageProjectionPending{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.MeteringUsageProjectionPending{}).Where("traffic_record_id = ?", 900000).Update("node_id", 99).Error; err != nil {
		t.Fatal(err)
	}
	if err := EnsureHourlyUsageReadable(db, TrafficScope{UserID: 8}, time.Time{}, time.Time{}); !errors.Is(err, metering.ErrUsageProjectionPending) {
		t.Fatal("inconsistent pending source appeared complete", err)
	}
}
