package application

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"time"
)

const FlowStartStatisticsJobID = "flow_start_statistics"

func (s *Services) FlowStartStatisticsJob() PeriodicJob {
	store := meteringstore.FlowStatistics{DB: s.Identity.db}
	return PeriodicJob{ID: FlowStartStatisticsJobID, Name: "连接开始事件统计投影", Interval: time.Second, Timeout: time.Minute, Immediate: true, ReconcileAfterLoss: true, CompactHistory: true, RetryFailures: true, Ready: store.HasPending, Run: func(ctx context.Context) error {
		n, err := s.observeProjectionStorage(func() (int, error) { return store.Drain(ctx, 200) })
		return projectionBatchOutcome(n, 200, err)
	}}
}
