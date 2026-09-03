package handler

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

// Bucket SQL yields DATETIME on MySQL and text on SQLite. Keep the wire value
// a UTC timestamp on both drivers instead of relying on driver type inference.
type trafficBucketTime struct{ time.Time }

func (t *trafficBucketTime) Scan(value any) error {
	if stamp, ok := value.(time.Time); ok {
		t.Time = stamp.UTC()
		return nil
	}
	var raw string
	switch value := value.(type) {
	case string:
		raw = value
	case []byte:
		raw = string(value)
	default:
		return fmt.Errorf("invalid traffic bucket timestamp type %T", value)
	}
	stamp, err := time.Parse("2006-01-02 15:04:05", raw)
	if err != nil {
		return err
	}
	t.Time = stamp.UTC()
	return nil
}

// GORM treats this scanner as a scalar time column, not an embedded model.
func (trafficBucketTime) GormDataType() string { return "time" }

func (b trafficUsageBucketSpec) forDB(db *gorm.DB) trafficUsageBucketSpec {
	if !datastore.IsSQLite(db) {
		return b
	}
	format := "%Y-%m-%d %H:%M:00"
	if b.Name == trafficUsageBucketHour {
		format = "%Y-%m-%d %H:00:00"
	}
	if b.Name == trafficUsageBucketDay {
		format = "%Y-%m-%d 00:00:00"
	}
	b.Expression = "strftime('" + format + "', record_at)"
	return b
}

func (b trafficUsageBucketSpec) group() string {
	return b.Expression + ", user_id, COALESCE(subscription_id, 0), node_id, protocol_multiplier_milli"
}

func (b trafficUsageBucketSpec) seekSource(query *gorm.DB, cursor *historyCursor) *gorm.DB {
	if cursor == nil {
		return query
	}
	width := time.Minute
	if b.Name == trafficUsageBucketHour {
		width = time.Hour
	}
	if b.Name == trafficUsageBucketDay {
		width = 24 * time.Hour
	}
	start := cursor.At.UTC().Truncate(width)
	// Retain the entire cursor bucket. Filtering raw IDs before MIN(id) and
	// SUM(...) would split a billable group and change totals/identity.
	if cursor.Direction == historyDirectionOlder {
		return query.Where("record_at < ?", start.Add(width))
	}
	return query.Where("record_at >= ?", start)
}
