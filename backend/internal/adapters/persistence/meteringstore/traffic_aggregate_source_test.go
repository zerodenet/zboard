package meteringstore

import (
	"context"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
	"time"
)

func trafficAggregateDB(t testing.TB) *gorm.DB {
	t.Helper()
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "aggregate.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	return db
}
func TestTrafficAggregateExactEdgesAndEveryScope(t *testing.T) {
	db := trafficAggregateDB(t)
	from := time.Date(2026, 9, 28, 1, 25, 0, 0, time.UTC)
	to := from.Add(5 * time.Hour)
	for i := -10; i <= 40; i++ {
		row := model.TrafficRecord{UserID: uint(i%2 + 3), SubscriptionID: uint(i%3 + 4), NodeID: 1, ProtocolEndpointID: 2, ReportID: fmt.Sprint(i), Nonce: fmt.Sprint(i), UsedBytes: int64(i + 20), RawBytes: 100, At: from.Add(time.Duration(i) * 10 * time.Minute)}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, scope := range []TrafficScope{{}, {UserID: 3}, {SubscriptionID: 4}, {NodeID: 1}, {ProtocolEndpointID: 2}, {UserID: 3, SubscriptionID: 4, NodeID: 1, ProtocolEndpointID: 2}, {NodeID: 9}} {
		for _, window := range [][2]time.Time{{from, to}, {from, from.Add(20 * time.Minute)}, {from.Truncate(time.Hour), to.Truncate(time.Hour)}} {
			var actual, expected struct{ Used, Raw, Count int64 }
			if err := TrafficAggregateSource(db, window[0], window[1], scope).Select("COALESCE(SUM(used_bytes),0) AS used, COALESCE(SUM(raw_bytes),0) AS raw, COALESCE(SUM(record_count),0) AS count").Scan(&actual).Error; err != nil {
				t.Fatal(err)
			}
			raw := db.Model(&model.TrafficRecord{}).Where("record_at >= ? AND record_at < ?", window[0], window[1])
			for _, f := range []struct {
				c  string
				id uint
			}{{"user_id", scope.UserID}, {"subscription_id", scope.SubscriptionID}, {"node_id", scope.NodeID}, {"protocol_endpoint_id", scope.ProtocolEndpointID}} {
				if f.id > 0 {
					raw = raw.Where(f.c+" = ?", f.id)
				}
			}
			if err := raw.Select("COALESCE(SUM(used_bytes),0) AS used, COALESCE(SUM(raw_bytes),0) AS raw, COUNT(*) AS count").Scan(&expected).Error; err != nil {
				t.Fatal(err)
			}
			if actual != expected {
				t.Fatalf("scope %+v window %v: %+v != %+v", scope, window, actual, expected)
			}
		}
	}
}
func BenchmarkTrafficMonthlyAggregation(b *testing.B) {
	db := trafficAggregateDB(b)
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(30 * 24 * time.Hour)
	// 300,000 accepted samples across thirty user/node scopes; ingestion is outside timing.
	if err := db.Exec(`WITH RECURSIVE samples(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM samples WHERE i < 299999) INSERT INTO traffic_records(user_id,subscription_id,node_id,report_id,used_bytes,raw_bytes,record_at) SELECT i%10+1, i%10+1, i%3+1, CAST(i AS TEXT), 1000, 1200, datetime(?, '+' || CAST(i*86400*30/300000 AS INTEGER) || ' seconds') || '.001' FROM samples`, from.Format("2006-01-02 15:04:05")).Error; err != nil {
		b.Fatal(err)
	}
	var summaryRows int64
	db.Table("traffic_usage_hourly").Count(&summaryRows)
	b.Logf("ledger samples=300000 hourly rows=%d", summaryRows)
	for _, source := range []string{"ledger", "hourly"} {
		b.Run(source, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				var total int64
				query := db.WithContext(context.Background()).Table("traffic_records").Where("record_at >= ? AND record_at < ?", from, to)
				if source == "hourly" {
					query = TrafficAggregateSource(db, from, to, TrafficScope{})
				}
				if err := query.Select("SUM(used_bytes)").Scan(&total).Error; err != nil {
					b.Fatal(err)
				}
				if total != 300000000 {
					b.Fatalf("wrong total %d", total)
				}
			}
		})
	}
}

func TestTrafficDailyTrendPreservesQuarterHourTimezoneBoundaries(t *testing.T) {
	db := trafficAggregateDB(t)
	user := model.User{Email: "calendar@example.test", Password: "unused", Status: "active", IsAdmin: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	location, err := time.LoadLocation("Asia/Kathmandu")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 9, 27, 0, 0, 0, 0, location)
	boundary := from.AddDate(0, 0, 1)
	for i, at := range []time.Time{boundary.Add(-10 * time.Minute), boundary.Add(10 * time.Minute)} {
		row := model.TrafficRecord{UserID: user.ID, NodeID: 1, ReportID: fmt.Sprint(i), Nonce: fmt.Sprint(i), UsedBytes: int64(i+1) * 100, At: at.UTC()}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	q := metering.TrafficTrendQuery{Administrative: true, From: from, Days: 2, Timezone: location.String()}
	for i := 0; i < 2; i++ {
		day := from.AddDate(0, 0, i)
		q.Buckets = append(q.Buckets, metering.TrendBucket{Key: day.Format("2006-01-02"), StartUTC: day.UTC(), EndUTC: day.AddDate(0, 0, 1).UTC()})
	}
	result, err := (metering.TrafficTrends{Repository: TrafficTrends{DB: db}}).Read(context.Background(), user.ID, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snapshot.Points) != 2 || result.Snapshot.Points[0].UsedBytes != 100 || result.Snapshot.Points[1].UsedBytes != 200 {
		t.Fatalf("calendar points %+v", result.Snapshot.Points)
	}
}
