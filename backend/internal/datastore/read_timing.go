package datastore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/readtiming"
	"gorm.io/gorm/logger"
)

// Query elapsed time includes connection acquisition and execution. It is not
// labelled as pool-wait time; pool-wide waits remain in runtime diagnostics.
type readTimingLogger struct{ logger.Interface }

func (l readTimingLogger) LogMode(level logger.LogLevel) logger.Interface {
	return readTimingLogger{l.Interface.LogMode(level)}
}
func (l readTimingLogger) Trace(ctx context.Context, begin time.Time, sql func() (string, int64), err error) {
	readtiming.Record(ctx, "sql", time.Since(begin))
	l.Interface.Trace(ctx, begin, sql, err)
}
