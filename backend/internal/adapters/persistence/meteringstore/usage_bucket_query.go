package meteringstore

import (
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

func (b UsageBucketSpec) ForDB(db *gorm.DB) UsageBucketSpec {
	if !datastore.IsSQLite(db) {
		return b
	}
	b.Expression = datastore.SQLiteTrafficBucketExpression(b.Name)
	return b
}

// Keep the exact raw-time scope while adding an indexed bucket range. The
// matching SQLite index can then stream groups without a temporary sort tree.
func (b UsageBucketSpec) GroupSource(query *gorm.DB, window UsageWindow) *gorm.DB {
	if !datastore.IsSQLite(query) {
		return query
	}
	from := window.From.UTC().Truncate(b.Width()).Format("2006-01-02 15:04:05")
	last := window.To.UTC().Add(-time.Nanosecond).Truncate(b.Width()).Format("2006-01-02 15:04:05")
	return query.Where(b.Expression+" >= ? AND "+b.Expression+" <= ?", from, last)
}

func (b UsageBucketSpec) Group() string {
	return b.Expression + ", user_id, COALESCE(subscription_id, 0), node_id, protocol_multiplier_milli"
}

func (b UsageBucketSpec) Width() time.Duration {
	width := time.Minute
	if b.Name == "hour" {
		width = time.Hour
	}
	if b.Name == "day" {
		width = 24 * time.Hour
	}
	return width
}

func (b UsageBucketSpec) SeekSource(query *gorm.DB, cursor *usageCursor) *gorm.DB {
	if cursor == nil {
		return query
	}
	width := b.Width()
	start := cursor.At.UTC().Truncate(width)
	// Retain the entire cursor bucket. Filtering raw IDs before MIN(id) and
	// SUM(...) would split a billable group and change totals/identity.
	if cursor.Direction == "older" {
		return query.Where("record_at < ?", start.Add(width))
	}
	return query.Where("record_at >= ?", start)
}
