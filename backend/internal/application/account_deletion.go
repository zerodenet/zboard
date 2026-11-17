package application

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/identitystore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"time"
)

const UserCleanupJobID = "user_cleanup"

func (s *Services) AccountDeletion() identity.Deletion {
	return identity.Deletion{Repository: identitystore.Deletion{DB: s.Identity.db, Publish: networkstore.EnqueueNodePublication}}
}
func (s *Services) UserCleanupJob() PeriodicJob {
	repository := identitystore.Deletion{DB: s.Identity.db, Publish: networkstore.EnqueueNodePublication}
	return PeriodicJob{ID: UserCleanupJobID, Name: "无有效订阅账号清理", Interval: time.Minute, Timeout: 50 * time.Second,
		CompactHistory: true, ReconcileAfterLoss: true, RetryFailures: true,
		Ready: repository.CleanupEnabled,
		Run:   func(ctx context.Context) error { _, err := repository.Cleanup(ctx, time.Now().UTC(), 100); return err },
	}
}
