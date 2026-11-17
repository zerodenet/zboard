package meteringstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestUserActivityUsesOneBatchAndExistingCalendarProjections(t *testing.T) {
	for _, zone := range []string{"Asia/Shanghai", "Asia/Kathmandu", "America/New_York"} {
		t.Run(zone, func(t *testing.T) {
			db := trafficAggregateDB(t)
			q := calendarQuery(t, zone, "2026-11-01", 7)
			for i, b := range q.Buckets {
				for _, user := range []uint{7, 8, 99} {
					for _, sub := range []uint{21, 22} {
						for j, at := range []time.Time{b.StartUTC, b.EndUTC.Add(-time.Millisecond)} {
							id := fmt.Sprintf("%d-%d-%d-%d", i, user, sub, j)
							row := model.TrafficRecord{UserID: user, SubscriptionID: sub, NodeID: 1, ReportID: id, Nonce: id, At: at, UsedBytes: int64(i+1) * 5, RawBytes: 1}
							if err := db.Create(&row).Error; err != nil {
								t.Fatal(err)
							}
						}
					}
				}
			}
			if err := db.Transaction(datastore.RebuildTrafficHourly); err != nil {
				t.Fatal(err)
			}
			// No live subscription is required: archived/ended subscription usage remains included.
			capture := &endpointUsageSQLCapture{Interface: logger.Discard}
			rows, err := loadUserTrafficActivity(db.Session(&gorm.Session{Logger: capture}), []uint{7, 8}, q)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 14 {
				t.Fatal(rows)
			}
			if !strings.Contains(capture.statement, "traffic_usage_hourly") || !strings.Contains(capture.statement, "user_id IN (7,8)") {
				t.Fatal("batch predicate/projection missing", capture.statement)
			}
			for _, r := range rows {
				if r.UserID != 7 && r.UserID != 8 {
					t.Fatal("foreign user leaked", r)
				}
				found := false
				for i, b := range q.Buckets {
					if r.Day[:10] == b.Key {
						found = true
						if r.UsedBytes != int64(i+1)*20 {
							t.Fatal(r)
						}
					}
				}
				if !found {
					t.Fatal(r)
				}
			}
		})
	}
}

func TestUserActivityLateReportsOverlayWithoutWritesOrDoubleCounting(t *testing.T) {
	db := trafficAggregateDB(t)
	if err := db.Create(&model.User{ID: 1, Email: "activity-admin@example.test", Password: "unused", Status: "active", IsAdmin: true}).Error; err != nil {
		t.Fatal(err)
	}
	service := metering.UserActivity{Repository: TrafficTrends{DB: db}}
	location, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, location)
	add := func(key string, used int64) {
		t.Helper()
		row := model.TrafficRecord{UserID: 7, SubscriptionID: 999, NodeID: 1, ReportID: key, Nonce: key, At: now.AddDate(0, 0, -4), UsedBytes: used}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			return RecordUsageProjections(tx, []model.TrafficRecord{row})
		}); err != nil {
			t.Fatal(err)
		}
	}
	read := func(want int64) {
		t.Helper()
		out, err := service.Read(context.Background(), 1, []uint{7, 8}, location, now)
		if err != nil {
			t.Fatal(err)
		}
		if out.From != "2026-11-04" || out.To != "2026-11-10" || out.Timezone != "Asia/Shanghai" || len(out.Items) != 2 {
			t.Fatal(out)
		}
		if out.Items[0].UsedBytes != want || len(out.Items[0].Days) != 7 || out.Items[0].Days[2].UsedBytes != want || out.Items[1].UsedBytes != 0 {
			t.Fatal(out)
		}
	}
	add("accepted-before-rollup", 10)
	read(10)
	if n, err := (UsageProjectionWorker{DB: db}).Drain(context.Background(), 200); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	read(10)
	add("historical-backfill", 25)
	// A read cannot drain the pending checkpoint or create any new data.
	var before, after int64
	db.Model(&model.MeteringUsageProjectionPending{}).Count(&before)
	read(35)
	db.Model(&model.MeteringUsageProjectionPending{}).Count(&after)
	if before != after {
		t.Fatal("read changed pending records")
	}
	if _, err := (UsageProjectionWorker{DB: db}).Drain(context.Background(), 200); err != nil {
		t.Fatal(err)
	}
	read(35)
	// Re-authorize using current primary state even for an otherwise valid batch.
	db.Model(&model.User{}).Where("id=1").Update("is_admin", false)
	if _, err := service.Read(context.Background(), 1, []uint{7}, location, now); !errors.Is(err, metering.ErrTrendPermission) {
		t.Fatal(err)
	}
}

func TestUserActivityScopesPendingBudgetToRequestedUsers(t *testing.T) {
	db := trafficAggregateDB(t)
	q := calendarQuery(t, "UTC", "2026-11-04", 7)
	at := q.Buckets[2].StartUTC.Add(time.Hour)
	// An unrelated user's invalid pending checkpoint must not make this page unknown.
	record := model.TrafficRecord{UserID: 99, NodeID: 1, At: at, UsedBytes: 1, ReportID: "foreign-pending", Nonce: "foreign-pending"}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MeteringUsageProjectionPending{TrafficRecordID: record.ID, UserID: 99, NodeID: 1, RecordAt: &at, HourlyPending: true, DeltaUsedBytes: 1}).Error; err != nil {
		t.Fatal(err)
	}
	windows := [][2]time.Time{{q.Buckets[0].StartUTC, q.Buckets[6].EndUTC}}
	if _, err := inspectHourlyUsageWindowsForUsers(db, TrafficScope{}, windows, []uint{7, 8}); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectHourlyUsageWindowsForUsers(db, TrafficScope{}, windows, []uint{99}); !errors.Is(err, metering.ErrUsageProjectionPending) {
		t.Fatal(err)
	}
}
