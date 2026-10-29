package meteringstore

import (
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

func calendarQuery(t testing.TB, timezone, date string, days int) metering.TrafficTrendQuery {
	t.Helper()
	location, err := time.LoadLocation(timezone)
	if err != nil {
		t.Fatal(err)
	}
	from, err := time.ParseInLocation("2006-01-02", date, location)
	if err != nil {
		t.Fatal(err)
	}
	q := metering.TrafficTrendQuery{From: from, Days: days, Timezone: timezone}
	for i := 0; i < days; i++ {
		day := from.AddDate(0, 0, i)
		q.Buckets = append(q.Buckets, metering.TrendBucket{Key: day.Format("2006-01-02"), StartUTC: day.UTC(), EndUTC: day.AddDate(0, 0, 1).UTC()})
	}
	return q
}

func TestTrafficCalendarProjectionPreservesEveryLocalDayAndScope(t *testing.T) {
	for _, timezone := range []string{"UTC", "Asia/Shanghai", "Asia/Kathmandu", "Australia/Adelaide", "Australia/Lord_Howe", "America/New_York"} {
		t.Run(timezone, func(t *testing.T) {
			db := trafficAggregateDB(t)
			q := calendarQuery(t, timezone, "2026-10-03", 3)
			// Include midnight edges, sub-second precision, daylight-saving
			// transitions, and foreign owners/endpoints in the same hours.
			for i, bucket := range q.Buckets {
				for j, at := range []time.Time{bucket.StartUTC.Add(-time.Nanosecond), bucket.StartUTC, bucket.StartUTC.Add(12 * time.Hour), bucket.EndUTC.Add(-time.Nanosecond), bucket.EndUTC} {
					for user := uint(1); user <= 2; user++ {
						id := fmt.Sprintf("%d-%d-%d", i, j, user)
						record := model.TrafficRecord{UserID: user, SubscriptionID: user, NodeID: user, ProtocolEndpointID: user, ReportID: id, Nonce: id, At: at, UploadBytes: 3, DownloadBytes: 7, UsedBytes: 17}
						if err := db.Create(&record).Error; err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			if err := db.Transaction(datastore.RebuildTrafficHourly); err != nil {
				t.Fatal(err)
			}
			for _, scope := range []TrafficScope{{}, {UserID: 1}, {SubscriptionID: 1}, {NodeID: 1}, {ProtocolEndpointID: 1}, {UserID: 1, NodeID: 2}} {
				q.UserID, q.SubscriptionID, q.NodeID, q.ProtocolEndpointID = scope.UserID, scope.SubscriptionID, scope.NodeID, scope.ProtocolEndpointID
				capture := &endpointUsageSQLCapture{Interface: logger.Discard}
				rows, err := loadTrafficCalendar(db.Session(&gorm.Session{Logger: capture}), q)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(capture.statement, "traffic_usage_hourly") {
					t.Fatal("calendar bypassed the hourly projection", capture.statement)
				}
				byDay := map[string]metering.TrafficTrendAggregate{}
				for _, row := range rows {
					// The SQLite driver can materialize DATE/label values as
					// RFC3339. BuildTrafficTrendPoints applies the same normalization.
					if len(row.Day) < 10 {
						t.Fatalf("calendar row has no day: %+v; query: %s", row, capture.statement)
					}
					row.Day = row.Day[:10]
					byDay[row.Day] = row
				}
				for _, bucket := range q.Buckets {
					var expected metering.TrafficTrendAggregate
					query := db.Table("traffic_records").Where("record_at >= ? AND record_at < ?", bucket.StartUTC, bucket.EndUTC)
					for _, filter := range []struct {
						column string
						id     uint
					}{{"user_id", scope.UserID}, {"subscription_id", scope.SubscriptionID}, {"node_id", scope.NodeID}, {"protocol_endpoint_id", scope.ProtocolEndpointID}} {
						if filter.id > 0 {
							query = query.Where(filter.column+" = ?", filter.id)
						}
					}
					if err := query.Select("? AS day, SUM(upload_bytes) AS upload_bytes, SUM(download_bytes) AS download_bytes, SUM(used_bytes) AS used_bytes, COUNT(*) AS record_count", bucket.Key).Scan(&expected).Error; err != nil {
						t.Fatal(err)
					}
					expected.Day = bucket.Key
					actual := byDay[bucket.Key]
					actual.Day = bucket.Key // Grouped queries omit empty days.
					if actual != expected {
						t.Fatalf("scope %+v day %s: %+v != %+v", scope, bucket.Key, actual, expected)
					}
				}
			}
		})
	}
}

func TestTrafficCalendarMaximumFractionalTimezoneRange(t *testing.T) {
	db := trafficAggregateDB(t)
	q := calendarQuery(t, "Asia/Kathmandu", "2026-01-01", 366)
	rows, err := loadTrafficCalendar(db, q)
	if err != nil || len(rows) != 366 {
		t.Fatalf("maximum range: rows=%d: %v", len(rows), err)
	}
	for _, row := range rows {
		if row.UsedBytes != 0 || row.RecordCount != 0 {
			t.Fatalf("empty day contains usage: %+v", row)
		}
	}
}
