package meteringstore

import (
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
)

const trafficCalendarSums = "COALESCE(SUM(upload_bytes), 0) AS upload_bytes, COALESCE(SUM(download_bytes), 0) AS download_bytes, COALESCE(SUM(used_bytes), 0) AS used_bytes, COALESCE(SUM(record_count), 0) AS record_count"

func loadTrafficCalendar(db *gorm.DB, q metering.TrafficTrendQuery) ([]metering.TrafficTrendAggregate, error) {
	var rows []metering.TrafficTrendAggregate
	err := trafficCalendarQuery(db, q).Scan(&rows).Error
	return rows, err
}

func trafficCalendarQuery(db *gorm.DB, q metering.TrafficTrendQuery) *gorm.DB {
	scope := TrafficScope{q.UserID, q.SubscriptionID, q.NodeID, q.ProtocolEndpointID}
	aligned := true
	for _, bucket := range q.Buckets {
		if !bucket.StartUTC.Equal(bucket.StartUTC.Truncate(time.Hour)) || !bucket.EndUTC.Equal(bucket.EndUTC.Truncate(time.Hour)) {
			aligned = false
			break
		}
	}
	if !aligned {
		// A local midnight can split a UTC hour. Build each calendar day from
		// complete hourly buckets plus its exact ledger edges, rather than
		// falling back to a full-range ledger scan. Adjacent edge ranges are
		// disjoint even when they share an hour; no sample is counted twice.
		parts := make([]string, 0, len(q.Buckets))
		queries := make([]interface{}, 0, len(q.Buckets))
		for _, bucket := range q.Buckets {
			query := TrafficAggregateSource(db, bucket.StartUTC, bucket.EndUTC, scope).
				Select("? AS day, "+trafficCalendarSums, bucket.Key)
			parts = append(parts, "SELECT * FROM (?) AS calendar_day")
			queries = append(queries, query)
		}
		// The capability bounds this to 366 days, below SQLite's 500-term
		// compound-select limit. All days execute in one SQL statement.
		return db.Raw(strings.Join(parts, " UNION ALL "), queries...)
	}
	query := TrafficAggregateSource(db, q.Buckets[0].StartUTC, q.Buckets[len(q.Buckets)-1].EndUTC, scope)
	expression := "DATE(record_at)"
	var args []interface{}
	if q.Timezone != "UTC" {
		parts := make([]string, 0, len(q.Buckets))
		for _, bucket := range q.Buckets {
			parts = append(parts, "WHEN record_at >= ? AND record_at < ? THEN ?")
			// Aligned sources contain hourly TEXT keys on SQLite. The driver's
			// time.Time encoding differs from those keys, so midnight's bucket
			// can compare before its own lower bound and lose its day.
			args = append(args, bucket.StartUTC.UTC().Format("2006-01-02 15:04:05"), bucket.EndUTC.UTC().Format("2006-01-02 15:04:05"), bucket.Key)
		}
		expression = "CASE " + strings.Join(parts, " ") + " ELSE NULL END"
	}
	return query.Select(expression+" AS day, "+trafficCalendarSums, args...).Group("day").Order("day ASC")
}
