package jobstore

import (
	"context"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func (s *Store) HasUnresolvedResource(ctx context.Context, resource string) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&Record{}).
		Where("resource = ? AND state IN ?", resource, []jobs.State{jobs.Queued, jobs.RetryWait, jobs.Running, jobs.CancelRequested, jobs.Unknown}).
		Count(&count).Error
	return count != 0, err
}
