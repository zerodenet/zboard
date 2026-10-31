package meteringstore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestHourlyProjectionReadinessIsScopedIndexedAndIncludesEndpointZero(t *testing.T) {
	db := trafficAggregateDB(t)
	at := time.Date(2026, 10, 20, 1, 15, 0, 0, time.UTC)
	record := model.TrafficRecord{UserID: 7, SubscriptionID: 2, NodeID: 3, ProtocolEndpointID: 0, ReportID: "unattributed-endpoint", Nonce: "unattributed-endpoint", At: at, RawBytes: 7, UsedBytes: 11, ProtocolMultiplierMilli: 1500}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		return RecordUsageProjections(tx, []model.TrafficRecord{record})
	}); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		scope    TrafficScope
		from, to time.Time
		pending  bool
	}{
		{TrafficScope{}, time.Time{}, time.Time{}, true},
		{TrafficScope{UserID: 7}, time.Time{}, time.Time{}, true},
		{TrafficScope{UserID: 8}, time.Time{}, time.Time{}, false},
		{TrafficScope{SubscriptionID: 2}, at.Truncate(time.Hour), at.Truncate(time.Hour).Add(time.Hour), true},
		{TrafficScope{SubscriptionID: 9}, at, at.Add(time.Hour), false},
		{TrafficScope{NodeID: 3}, at.Truncate(time.Hour), at.Truncate(time.Hour).Add(time.Hour), true},
		{TrafficScope{ProtocolEndpointID: 9}, at, at.Add(time.Hour), false},
		{TrafficScope{UserID: 7}, at.Add(time.Hour), at.Add(2 * time.Hour), false},
		{TrafficScope{UserID: 7}, at.Add(-time.Hour), at, false},
	} {
		err := EnsureHourlyUsageReady(db, check.scope, check.from, check.to)
		if check.pending && !errors.Is(err, metering.ErrUsageProjectionPending) || !check.pending && err != nil {
			t.Fatalf("scope=%+v range=%v..%v pending=%t err=%v", check.scope, check.from, check.to, check.pending, err)
		}
	}
	var plan []struct{ Detail string }
	if err := db.Raw("EXPLAIN QUERY PLAN SELECT traffic_record_id FROM metering_usage_projection_pending WHERE hourly_pending=1 AND user_id=7 AND record_at>=? AND record_at<? LIMIT 1", at.Truncate(time.Hour), at.Truncate(time.Hour).Add(time.Hour)).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	indexed := false
	for _, row := range plan {
		if strings.Contains(row.Detail, "idx_usage_hourly_pending_user") {
			indexed = true
		}
	}
	if !indexed {
		t.Fatalf("pending readiness lost its scope/time bound: %+v", plan)
	}
	if count, err := (UsageProjectionWorker{DB: db}).Drain(context.Background(), 200); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := EnsureHourlyUsageReady(db, TrafficScope{}, time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	var used, daily int64
	if err := db.Table("traffic_usage_hourly").Select("SUM(used_bytes)").Scan(&used).Error; err != nil || used != 11 {
		t.Fatal(used, err)
	}
	if err := db.Model(&model.ProtocolEndpointUsageDaily{}).Count(&daily).Error; err != nil || daily != 0 {
		t.Fatal("endpoint-zero created a daily endpoint projection", daily, err)
	}
}

func TestHourlyProjectionPartialHourGuardMatchesActualAggregateEdges(t *testing.T) {
	db := trafficAggregateDB(t)
	hour := time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)
	for i, minute := range []int{15, 45} {
		identity := fmt.Sprint(i)
		record := model.TrafficRecord{NodeID: 3, UserID: 7, SubscriptionID: 2, ProtocolEndpointID: 9, ReportID: identity, Nonce: identity, At: hour.Add(time.Duration(minute) * time.Minute), RawBytes: 11, UsedBytes: 11, ProtocolMultiplierMilli: 1000}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
			return RecordUsageProjections(tx, []model.TrafficRecord{record})
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, window := range []struct {
		from, to time.Time
		want     int64
		pending  bool
	}{
		{hour.Add(30 * time.Minute), hour.Add(150 * time.Minute), 11, false},
		{hour.Add(30 * time.Minute), hour.Add(90 * time.Minute), 11, false},
		{hour.Add(-30 * time.Minute), hour.Add(30 * time.Minute), 11, false},
		{hour.Add(-30 * time.Minute), hour.Add(90 * time.Minute), 22, true},
	} {
		err := EnsureHourlyUsageReady(db, TrafficScope{UserID: 7}, window.from, window.to)
		if window.pending {
			if !errors.Is(err, metering.ErrUsageProjectionPending) {
				t.Fatal("full pending hour appeared ready", err)
			}
			continue
		}
		if err != nil {
			t.Fatal("precise ledger edge incorrectly waited for hourly rollup", err)
		}
		var used int64
		if err := TrafficAggregateSource(db, window.from, window.to, TrafficScope{UserID: 7}).Select("SUM(used_bytes)").Scan(&used).Error; err != nil || used != window.want {
			t.Fatal(used, window.want, err)
		}
	}
}

func TestHourlyProjectionCalendarMidnightEdgesDoNotBlockOtherFullHours(t *testing.T) {
	for _, timezone := range []string{"Asia/Kolkata", "Asia/Kathmandu"} {
		t.Run(timezone, func(t *testing.T) {
			db := trafficAggregateDB(t)
			if err := db.Create(&model.User{ID: 7, Email: "calendar-pending@example.test", Password: "unused", Status: "active"}).Error; err != nil {
				t.Fatal(err)
			}
			q := calendarQuery(t, timezone, "2026-10-19", 2)
			boundary := q.Buckets[0].EndUTC
			appendRecord := func(identity string, at time.Time, used int64) {
				t.Helper()
				record := model.TrafficRecord{NodeID: 3, UserID: 7, SubscriptionID: 2, ProtocolEndpointID: 9, ReportID: identity, Nonce: identity, At: at, RawBytes: used, UsedBytes: used, ProtocolMultiplierMilli: 1000}
				if err := db.Transaction(func(tx *gorm.DB) error {
					if err := tx.Create(&record).Error; err != nil {
						return err
					}
					return RecordUsageProjections(tx, []model.TrafficRecord{record})
				}); err != nil {
					t.Fatal(err)
				}
			}
			appendRecord("before", boundary.Add(-10*time.Minute), 7)
			appendRecord("after", boundary.Add(10*time.Minute), 11)
			service := metering.TrafficTrends{Repository: TrafficTrends{DB: db}}
			out, err := service.Read(context.Background(), 7, q)
			if err != nil || out.Snapshot.RecordCount != 2 || out.Snapshot.Points[0].UsedBytes != 7 || out.Snapshot.Points[1].UsedBytes != 11 {
				t.Fatal("interior calendar edge was treated as a missing hourly bucket", out, err)
			}
			appendRecord("complete-hour", q.Buckets[0].StartUTC.Truncate(time.Hour).Add(2*time.Hour), 13)
			if out, err := service.Read(context.Background(), 7, q); err != nil || out.Snapshot.RecordCount != 3 || out.Snapshot.Points[0].UsedBytes != 20 || out.Snapshot.Points[1].UsedBytes != 11 {
				t.Fatal("calendar lost bounded unprojected complete hour", out, err)
			}
			if count, err := (UsageProjectionWorker{DB: db}).Drain(context.Background(), 200); err != nil || count != 3 {
				t.Fatal(count, err)
			}
			out, err = service.Read(context.Background(), 7, q)
			if err != nil || out.Snapshot.RecordCount != 3 || out.Snapshot.Points[0].UsedBytes != 20 || out.Snapshot.Points[1].UsedBytes != 11 {
				t.Fatal("calendar changed after projection", out, err)
			}
		})
	}
}

func TestHourlyProjectionRejectsSQLiteStoredBucketOverflowWithoutLosingIntent(t *testing.T) {
	db := usageProjectionDB(t, filepath.Join(t.TempDir(), "hourly-overflow.db"))
	enqueueProjectionRecord(t, db, 1, 1)
	if err := db.Table("traffic_usage_hourly").Create(map[string]any{"record_at": "2026-10-20 01:00:00", "user_id": 0, "subscription_id": 0, "node_id": 1, "protocol_endpoint_id": 3, "protocol_multiplier_milli": 1500, "used_bytes": int64(math.MaxInt64), "record_count": 0}).Error; err != nil {
		t.Fatal(err)
	}
	if count, err := (UsageProjectionWorker{DB: db}).Drain(context.Background(), 200); err == nil || count != 0 {
		t.Fatal("SQLite promoted exact hourly integer to REAL", count, err)
	}
	projectionCounts(t, db, 1, 1, 0)
	var stored int64
	if err := db.Table("traffic_usage_hourly").Select("used_bytes").Scan(&stored).Error; err != nil || stored != math.MaxInt64 {
		t.Fatal("failed projection changed confirmed bucket", stored, err)
	}
}
