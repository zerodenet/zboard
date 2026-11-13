package application

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"time"
)

const LedgerRetentionJobID = "metering_ledger_retention"

func (s *Services) LedgerRetentionJob() PeriodicJob {
	store := meteringstore.LedgerRetention{DB: s.Identity.db}
	return PeriodicJob{ID: LedgerRetentionJobID, Name: "流量明细合并与淘汰", Interval: time.Second, Timeout: time.Minute, Immediate: true, ReconcileAfterLoss: true, CompactHistory: true, RetryFailures: true, Ready: store.HasPending,
		Run: func(ctx context.Context) error {
			n, err := s.observeProjectionStorage(func() (int, error) { return store.Drain(ctx, 100) })
			return projectionBatchOutcome(n, 100, err)
		}}
}
