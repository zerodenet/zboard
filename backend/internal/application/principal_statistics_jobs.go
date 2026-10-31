package application

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"time"
)

const PrincipalStatisticsJobID = "principal_statistics"
const PrincipalScopeRetentionJobID = "principal_scope_retention"

func (s *Services) PrincipalStatisticsJob() PeriodicJob {
	store := meteringstore.PrincipalStatistics{DB: s.Identity.db}
	return PeriodicJob{ID: PrincipalStatisticsJobID, Name: "连接统计投影", Interval: time.Second, Timeout: time.Minute, Immediate: true, ReconcileAfterLoss: true, CompactHistory: true, RetryFailures: true,
		Ready: store.HasPending,
		Run: func(ctx context.Context) error {
			n, err := s.observeProjectionStorage(func() (int, error) { return store.Drain(ctx, 200) })
			return projectionBatchOutcome(n, 200, err)
		},
	}
}
func (s *Services) PrincipalScopeRetentionJob() PeriodicJob {
	store := meteringstore.PrincipalBucketMaintenance{DB: s.Identity.db}
	return PeriodicJob{ID: PrincipalScopeRetentionJobID, Name: "连接统计聚合与保留", Interval: time.Second, Timeout: time.Minute, Immediate: true, ReconcileAfterLoss: true, CompactHistory: true, RetryFailures: true,
		Ready: store.HasPending,
		Run: func(ctx context.Context) error {
			n, err := s.observeProjectionStorage(func() (int, error) { return store.Drain(ctx, 200) })
			return projectionBatchOutcome(n, 200, err)
		},
	}
}
