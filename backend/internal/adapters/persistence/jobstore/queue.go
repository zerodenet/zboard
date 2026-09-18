package jobstore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

// QueueStatus is the deployment-wide execution backlog, independent of the
// domain inboxes (events, desired node configurations or operation batches).
type QueueStatus struct {
	PendingLimit            int   `json:"pending_limit" gorm:"-"`
	PluginPendingLimit      int   `json:"plugin_pending_limit" gorm:"-"`
	PluginOwnerPendingLimit int   `json:"plugin_owner_pending_limit" gorm:"-"`
	MaintenanceReserved     bool  `json:"maintenance_reserved" gorm:"column:maintenance_reserved"`
	Pending                 int64 `json:"pending"`
	Running                 int64 `json:"running"`
	Delayed                 int64 `json:"delayed" gorm:"column:delayed_count"`
	Unknown                 int64 `json:"unknown"`
}

func (s *Store) QueueStatus(ctx context.Context) (QueueStatus, error) {
	out := QueueStatus{PendingLimit: jobs.MaxPending, PluginPendingLimit: jobs.MaxPluginPending, PluginOwnerPendingLimit: jobs.MaxPluginOwnerPending}
	now := s.now().UTC().Truncate(time.Millisecond)
	err := s.db.WithContext(ctx).Model(&Record{}).Where("state IN ?", []jobs.State{jobs.Queued, jobs.RetryWait, jobs.Running, jobs.CancelRequested, jobs.Unknown}).Select(`
 COALESCE(SUM(CASE WHEN state IN ('queued','retry_wait') AND not_before <= ? THEN 1 ELSE 0 END),0) AS pending,
 COALESCE(SUM(CASE WHEN state IN ('running','cancel_requested') AND expires_at > ? THEN 1 ELSE 0 END),0) AS running,
 COALESCE(SUM(CASE WHEN state IN ('queued','retry_wait') AND not_before > ? THEN 1 ELSE 0 END),0) AS delayed_count,
 COALESCE(SUM(CASE WHEN state = 'unknown' OR (state IN ('running','cancel_requested') AND expires_at <= ?) THEN 1 ELSE 0 END),0) AS unknown,
 COALESCE(MAX(CASE WHEN resource = ? THEN 1 ELSE 0 END),0) AS maintenance_reserved`, now, now, now, now, jobs.MaintenanceResource).Scan(&out).Error
	return out, err
}
