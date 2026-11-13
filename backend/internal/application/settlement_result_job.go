package application

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
)

const SettlementResultJobID = "metering_settlement_results"

func (s *Services) SettlementResultJob() PeriodicJob {
	store := meteringstore.SettlementResults{DB: s.Identity.db}
	return PeriodicJob{ID: SettlementResultJobID, Name: "历史流量结算汇总", Interval: time.Second, Timeout: time.Minute, Immediate: true, ReconcileAfterLoss: true, CompactHistory: true, RetryFailures: true, Ready: store.HasPending,
		Run: func(ctx context.Context) error {
			n, err := s.observeProjectionStorage(func() (int, error) { return store.Drain(ctx, 200) })
			return projectionBatchOutcome(n, 200, err)
		}}
}
