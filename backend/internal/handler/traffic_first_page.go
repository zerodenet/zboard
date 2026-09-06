package handler

import (
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

const trafficFirstPageProbeRows = 1024

// Bound the first page only when a small, indexed recent-row probe proves
// there are enough distinct buckets. Aggregate from the START of its oldest
// time bucket, never from a raw row ID: earlier rows in that bucket can change
// both MIN(id) and charged usage. The scalar subquery and page share one SQL
// snapshot, so concurrent inserts cannot invalidate the proof. The input carries
// dimension filters but no time window: the computed lower bound is clamped to
// window.From. A second static lower bound would make SQLite seek to that older
// bound and filter all intervening history instead of seeking to the probe.
func (b trafficUsageBucketSpec) firstPageSource(base *gorm.DB, window historyWindow, limit int) *gorm.DB {
	recent := applyHistoryWindow(base.Session(&gorm.Session{}), "record_at", window).
		Select("record_at, user_id, subscription_id, node_id, protocol_multiplier_milli").
		Order("record_at desc").Limit(trafficFirstPageProbeRows)
	groups := base.Session(&gorm.Session{NewDB: true}).Table("(?) AS traffic_recent_records", recent).
		Select(b.Expression + " AS bucket_at").Group(b.group())
	boundary := base.Session(&gorm.Session{NewDB: true}).Table("(?) AS traffic_recent_buckets", groups).
		Select("CASE WHEN COUNT(*) >= ? AND MIN(bucket_at) > ? THEN MIN(bucket_at) ELSE ? END", limit+1, window.From, window.From)
	if datastore.IsSQLite(base) {
		// Use the existing expression index to stream groups, instead of
		// sorting every recent raw record. Keep exact raw bounds for windows
		// starting or ending inside a bucket. Only one expression lower bound
		// is supplied, so the seek starts at the probe rather than window.From.
		floorBoundary := strings.ReplaceAll(b.Expression, "record_at", "(?)")
		last := window.To.UTC().Add(-time.Nanosecond).Truncate(b.width()).Format("2006-01-02 15:04:05")
		return applyHistoryWindow(base.Session(&gorm.Session{}), "record_at", window).
			Where(b.Expression+" >= "+floorBoundary+" AND "+b.Expression+" <= ?", boundary, last)
	}
	return base.Session(&gorm.Session{}).Where("record_at >= (?) AND record_at < ?", boundary, window.To)
}
