package meteringstore

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
	"strings"
	"time"
)

type UsageWindow struct{ From, To time.Time }

func applyUsageWindow(q *gorm.DB, column string, w UsageWindow) *gorm.DB {
	return q.Where(column+" >= ? AND "+column+" < ?", w.From, w.To)
}

type usageCursor = metering.RecordCursor
type UsageBucketSpec struct {
	Name       string
	Expression string
}

func ParseUsageBucket(raw string) (UsageBucketSpec, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "minute":
		return UsageBucketSpec{
			Name:       "minute",
			Expression: "CAST(DATE_FORMAT(record_at, '%Y-%m-%d %H:%i:00') AS DATETIME)",
		}, nil
	case "hour":
		return UsageBucketSpec{
			Name:       "hour",
			Expression: "CAST(DATE_FORMAT(record_at, '%Y-%m-%d %H:00:00') AS DATETIME)",
		}, nil
	case "day":
		return UsageBucketSpec{
			Name:       "day",
			Expression: "CAST(DATE_FORMAT(record_at, '%Y-%m-%d 00:00:00') AS DATETIME)",
		}, nil
	default:
		return UsageBucketSpec{}, fmt.Errorf("bucket must be minute, hour or day")
	}
}
