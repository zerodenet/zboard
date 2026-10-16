package datastore

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/readtiming"
	"gorm.io/gorm/logger"
)

func TestReadTimingLoggerDoesNotMaterializeSQLOrLoseLogMode(t *testing.T) {
	ctx, timings := readtiming.NewContext(context.Background())
	wrapped := readTimingLogger{logger.Discard}.LogMode(logger.Silent)
	wrapped.Trace(ctx, time.Now().Add(-time.Millisecond), func() (string, int64) { t.Fatal("timing materialized SQL secrets"); return "", 0 }, nil)
	if !strings.Contains(timings.Header(), "sql;dur=") {
		t.Fatal(timings.Header())
	}
}
