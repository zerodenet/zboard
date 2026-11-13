package application

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"time"
)

const ColdIdentityJobID = "metering_cold_identities"

func (s *Services) ColdIdentityJob() PeriodicJob {
	store := meteringstore.ColdIdentities{DB: s.Identity.db}
	return PeriodicJob{ID: ColdIdentityJobID, Name: "补报身份压缩", Interval: time.Second, Timeout: time.Minute, Immediate: true, ReconcileAfterLoss: true, CompactHistory: true, RetryFailures: true, Ready: store.HasPending, Run: func(ctx context.Context) error {
		n, err := s.observeProjectionStorage(func() (int, error) { return store.Drain(ctx, 200) })
		return projectionBatchOutcome(n, 200, err)
	}}
}
