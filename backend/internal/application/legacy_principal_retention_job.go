package application

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
)

const LegacyPrincipalRetentionJobID = "legacy_principal_retention"

func (s *Services) LegacyPrincipalRetentionJob() PeriodicJob {
	store := meteringstore.LegacyPrincipalRetention{DB: s.Identity.db}
	return PeriodicJob{ID: LegacyPrincipalRetentionJobID, Name: "旧连接观测保留", Interval: time.Second, Timeout: time.Minute, Immediate: true,
		ReconcileAfterLoss: true, CompactHistory: true, RetryFailures: true, Ready: store.HasPending,
		Run: func(ctx context.Context) error {
			n, err := s.observeProjectionStorage(func() (int, error) { return store.Drain(ctx, 200) })
			return projectionBatchOutcome(n, 200, err)
		},
	}
}
