package application

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/messagingstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func (s *Services) batchControl() jobstore.BatchControl {
	return jobstore.BatchControl{DB: s.Identity.db, MailRetryGuard: messagingstore.RequireReviewedMailRetry}
}
func (s *Services) BatchControl() jobs.BatchControl {
	return jobs.BatchControl{Repository: s.batchControl()}
}
func (s *Services) QueueInternalBatch(ctx context.Context, id uint) error {
	return s.batchControl().QueueInternal(ctx, id)
}
func (s *Services) ClaimBatch(ctx context.Context, actor, id uint, queuedOnly bool) (string, error) {
	return s.batchControl().Claim(ctx, actor, id, queuedOnly)
}
