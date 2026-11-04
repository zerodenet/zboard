package application

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
)

const FlowCursorCompactionJobID = "flow_cursor_compaction"

func (s *Services) FlowCursorCompactionJob() PeriodicJob {
	store := meteringstore.FlowCursorCompaction{DB: s.Identity.db}
	return PeriodicJob{ID: FlowCursorCompactionJobID, Name: "连接计费状态压缩", Interval: time.Second, Timeout: time.Minute, Immediate: true,
		ReconcileAfterLoss: true, CompactHistory: true, RetryFailures: true, Ready: store.HasPending,
		Run: func(ctx context.Context) error {
			n, err := s.observeProjectionStorage(func() (int, error) { return store.Drain(ctx, 200) })
			return projectionBatchOutcome(n, 200, err)
		},
	}
}
