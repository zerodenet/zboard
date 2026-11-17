package meteringstore

import (
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm/logger"
)

// Measures reporting SQL only, excluding fixture ingestion, authorization,
// network latency, and cache hits. Compare identical sums from the same ledger.
func BenchmarkTrafficReportingAggregation(b *testing.B) {
	db := trafficAggregateDB(b)
	db.Logger = logger.Discard
	q := calendarQuery(b, "Asia/Kathmandu", "2026-10-01", 7)
	from, to := q.Buckets[0].StartUTC, q.Buckets[len(q.Buckets)-1].EndUTC
	var calendarParts []string
	var calendarArgs []interface{}
	for _, bucket := range q.Buckets {
		calendarParts = append(calendarParts, "WHEN record_at >= ? AND record_at < ? THEN ?")
		calendarArgs = append(calendarArgs, bucket.StartUTC, bucket.EndUTC, bucket.Key)
	}
	calendarExpression := "CASE " + strings.Join(calendarParts, " ") + " ELSE NULL END"
	if err := db.Exec(`WITH RECURSIVE samples(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM samples WHERE i < 99999)
INSERT INTO traffic_records(user_id,subscription_id,node_id,report_id,nonce,used_bytes,raw_bytes,record_at)
SELECT i%2+1, i%2+1, 1, CAST(i AS TEXT), CAST(i AS TEXT), 1000, 1200,
datetime(?, '+' || CAST(i*604800/100000 AS INTEGER) || ' seconds') || '.001' FROM samples`, from.Format("2006-01-02 15:04:05")).Error; err != nil {
		b.Fatal(err)
	}
	if err := db.Transaction(datastore.RebuildTrafficHourly); err != nil {
		b.Fatal(err)
	}
	var buckets int64
	if err := db.Table("traffic_usage_hourly").Count(&buckets).Error; err != nil {
		b.Fatal(err)
	}
	b.Logf("ledger rows=100000 hourly rows=%d", buckets)
	for _, source := range []string{"summary_ledger", "summary_hourly", "calendar_ledger", "calendar_hourly_edges"} {
		b.Run(source, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var total int64
				switch source {
				case "summary_ledger", "summary_hourly":
					table := "traffic_records"
					if source == "summary_hourly" {
						table = "traffic_usage_hourly"
					}
					var sums struct{ Total, Today int64 }
					if err := db.Table(table).Select("SUM(used_bytes) AS total, SUM(CASE WHEN record_at >= ? THEN used_bytes ELSE 0 END) AS today", from.Add(24*time.Hour).Truncate(24*time.Hour).Format("2006-01-02 15:04:05")).Scan(&sums).Error; err != nil {
						b.Fatal(err)
					}
					total = sums.Total
				case "calendar_ledger":
					var rows []metering.TrafficTrendAggregate
					if err := db.Table("traffic_records").Where("record_at >= ? AND record_at < ?", from, to).
						Select(calendarExpression+" AS day, SUM(upload_bytes) AS upload_bytes, SUM(download_bytes) AS download_bytes, SUM(used_bytes) AS used_bytes, COUNT(*) AS record_count", calendarArgs...).Group("day").Order("day ASC").Scan(&rows).Error; err != nil {
						b.Fatal(err)
					}
					for _, row := range rows {
						total += row.UsedBytes
					}
				case "calendar_hourly_edges":
					rows, err := loadTrafficCalendar(db, q)
					if err != nil {
						b.Fatal(err)
					}
					for _, row := range rows {
						total += row.UsedBytes
					}
				}
				if total != 100000000 {
					b.Fatalf("total=%d", total)
				}
			}
		})
	}
}
