package metering

import (
	"context"
	"time"
)

type UsageStatistics struct {
	Total      int64            `json:"total"`
	Aggregates RecordAggregates `json:"aggregates"`
	Bucket     string           `json:"bucket"`
	AsOf       time.Time        `json:"as_of"`
}

// StatisticsCache contains display snapshots only, never authority decisions.
type StatisticsCache interface {
	Get(context.Context, [32]byte, func() (UsageStatistics, error)) (UsageStatistics, error)
}
