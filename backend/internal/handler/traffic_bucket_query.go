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
	b.Expression = datastore.SQLiteTrafficBucketExpression(b.Name)
	return b
}

// Keep the exact raw-time scope while adding an indexed bucket range. The
// matching SQLite index can then stream groups without a temporary sort tree.
func (b trafficUsageBucketSpec) groupSource(query *gorm.DB, window historyWindow) *gorm.DB {
	if !datastore.IsSQLite(query) {
		return query
	}
	from := window.From.UTC().Truncate(b.width()).Format("2006-01-02 15:04:05")
	last := window.To.UTC().Add(-time.Nanosecond).Truncate(b.width()).Format("2006-01-02 15:04:05")
	return query.Where(b.Expression+" >= ? AND "+b.Expression+" <= ?", from, last)
}

func (b trafficUsageBucketSpec) group() string {
	return b.Expression + ", user_id, COALESCE(subscription_id, 0), node_id, protocol_multiplier_milli"
}

func (b trafficUsageBucketSpec) width() time.Duration {
	width := time.Minute
	if b.Name == trafficUsageBucketHour {
		width = time.Hour
	}
	if b.Name == trafficUsageBucketDay {
		width = 24 * time.Hour
	}
	return width
}

func (b trafficUsageBucketSpec) seekSource(query *gorm.DB, cursor *historyCursor) *gorm.DB {
	if cursor == nil {
		return query
	}
	width := b.width()
	start := cursor.At.UTC().Truncate(width)
	// Retain the entire cursor bucket. Filtering raw IDs before MIN(id) and
	// SUM(...) would split a billable group and change totals/identity.
	if cursor.Direction == historyDirectionOlder {
		return query.Where("record_at < ?", start.Add(width))
	}
	return query.Where("record_at >= ?", start)
}
