package application

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"math"
	"time"

	"github.com/zerodenet/zboard/backend/internal/batchsql"
	"gorm.io/gorm"
)

func (s *Services) NativeBatchSQLDiagnostics(now time.Time, freshness time.Duration) batchsql.Snapshot {
	if s == nil {
		var empty batchsql.Metrics
		return empty.Snapshot(now, freshness)
	}
	return s.nativeBatchSQL.Snapshot(now, freshness)
}
func (s *Services) nativeAuthorityTransaction(ctx context.Context, input BufferedEventProjection, apply func(*gorm.DB) error) (err error) {
	var bytes uint64
	complete := true
	for _, source := range input.Sources {
		n := uint64(len(source.Payload))
		if n > math.MaxUint64-bytes {
			complete = false
			bytes = math.MaxUint64
			break
		}
		bytes += n
	}
	observed, collection := s.nativeBatchSQL.Begin(ctx, bytes)
	if !complete {
		collection.MarkIncomplete()
	}
	returned := false
	defer func() { collection.Finish(err, returned) }()
	// Retry only the owned report-identity race, after the entire failed
	// authority transaction rolled back. Each attempt gets a fresh snapshot.
	for attempt := 0; attempt < 3; attempt++ {
		if err = ctx.Err(); err != nil {
			break
		}
		err = s.Identity.db.WithContext(observed).Transaction(apply)
		if !errors.Is(err, meteringstore.ErrNativeReportClaimed) {
			break
		}
	}
	returned = true
	return err
}
