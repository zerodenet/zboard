package jobstore

import (
	"context"
	"time"
)

// NativeExecutionStatus is the persistence projection used by the runtime
// status page for on-demand core handlers. Keeping the grouped scan here avoids
// an aggregate and latest-run query for every handler.
type NativeExecutionStatus struct {
	Handler        string
	Runs           uint64
	Failures       uint64
	Running        int
	Pending        int
	Unknown        int
	LastState      string
	LastFinishedAt *time.Time
}

func (s *Store) NativeExecutionStatuses(ctx context.Context, handlers []string) ([]NativeExecutionStatus, error) {
	if len(handlers) == 0 {
		return []NativeExecutionStatus{}, nil
	}
	now := s.now().UTC().Truncate(time.Millisecond)
	var counts []NativeExecutionStatus
	if err := s.db.WithContext(ctx).Model(&Record{}).
		Where("owner = ? AND handler IN ?", "system", handlers).
		Group("handler").
		Select(`handler,
 COUNT(CASE WHEN token <> '' THEN 1 END) AS runs,
 COUNT(CASE WHEN state IN ('failed','unknown') THEN 1 END) AS failures,
 COUNT(CASE WHEN state IN ('running','cancel_requested') AND expires_at > ? THEN 1 END) AS running,
 COUNT(CASE WHEN state IN ('queued','retry_wait') THEN 1 END) AS pending,
 COUNT(CASE WHEN state = 'unknown' OR (state IN ('running','cancel_requested') AND expires_at <= ?) THEN 1 END) AS unknown`, now, now).
		Scan(&counts).Error; err != nil {
		return nil, err
	}
	byHandler := make(map[string]int, len(counts))
	for i := range counts {
		byHandler[counts[i].Handler] = i
	}
	var latest []Record
	if err := s.db.WithContext(ctx).Model(&Record{}).
		Where("owner = ? AND handler IN ? AND finished_at IS NOT NULL", "system", handlers).
		Where(`job_runs.id = (SELECT latest.id FROM job_runs AS latest
 WHERE latest.owner = job_runs.owner AND latest.handler = job_runs.handler AND latest.finished_at IS NOT NULL
 ORDER BY latest.finished_at DESC, latest.id DESC LIMIT 1)`).
		Find(&latest).Error; err != nil {
		return nil, err
	}
	for _, row := range latest {
		index, ok := byHandler[row.Handler]
		if !ok {
			continue
		}
		counts[index].LastState, counts[index].LastFinishedAt = row.State, row.FinishedAt
	}
	return counts, nil
}
