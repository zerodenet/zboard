package handler

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
	"time"
)

type trafficBucketTime = meteringstore.BucketTime

func (b trafficUsageBucketSpec) forDB(db *gorm.DB) trafficUsageBucketSpec {
	return trafficUsageBucketSpec(meteringstore.UsageBucketSpec(b).ForDB(db))
}
func (b trafficUsageBucketSpec) groupSource(q *gorm.DB, w historyWindow) *gorm.DB {
	return meteringstore.UsageBucketSpec(b).GroupSource(q, meteringstore.UsageWindow{From: w.From, To: w.To})
}
func (b trafficUsageBucketSpec) group() string { return meteringstore.UsageBucketSpec(b).Group() }
func (b trafficUsageBucketSpec) width() time.Duration {
	return meteringstore.UsageBucketSpec(b).Width()
}
func (b trafficUsageBucketSpec) seekSource(q *gorm.DB, c *historyCursor) *gorm.DB {
	var cursor *metering.RecordCursor
	if c != nil {
		cursor = &metering.RecordCursor{At: c.At, ID: c.ID, Direction: c.Direction}
	}
	return meteringstore.UsageBucketSpec(b).SeekSource(q, cursor)
}
func (b trafficUsageBucketSpec) firstPageSource(q *gorm.DB, w historyWindow, limit int) *gorm.DB {
	return meteringstore.UsageBucketSpec(b).FirstPageSource(q, meteringstore.UsageWindow{From: w.From, To: w.To}, limit)
}
func (b trafficUsageBucketSpec) selectedFirstPageQuery(q, page *gorm.DB, limit int) *gorm.DB {
	return meteringstore.UsageBucketSpec(b).SelectedFirstPageQuery(q, page, limit)
}

const trafficFirstPageProbeRows = meteringstore.UsageFirstPageProbeRows
