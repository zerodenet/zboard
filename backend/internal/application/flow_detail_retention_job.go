package application

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
)

const FlowDetailRetentionJobID = "flow_detail_retention"

func (s *Services) FlowDetailRetentionJob() PeriodicJob {
	store := meteringstore.FlowDetailRetention{DB: s.Identity.db}
	return PeriodicJob{ID: FlowDetailRetentionJobID, Name: "连接明细与分钟统计保留", Interval: time.Second, Timeout: time.Minute, Immediate: true,
		ReconcileAfterLoss: true, CompactHistory: true, RetryFailures: true, Ready: store.HasPending,
		Run: func(ctx context.Context) error {
			n, err := s.observeProjectionStorage(func() (int, error) { return store.Drain(ctx, 200) })
			return projectionBatchOutcome(n, 200, err)
		},
	}
}
