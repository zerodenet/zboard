package jobstore

import (
	"context"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"gorm.io/gorm"
)

// ProjectionActivity is bounded cross-process execution metadata. Runtime here
// conservatively includes all work after Claim, not a claim of measured SQL
// latency. Missing/inconsistent metadata is unknown and must not grant capacity.
type ProjectionActivity struct {
	Known   bool
	Active  int
	Longest time.Duration
}

// handlers is a fixed application inventory, bounded to the five currently
// supported jobs. Invalid or duplicate inventory is unknown, never zero load.
func InspectProjectionActivity(ctx context.Context, db *gorm.DB, handlers []string, now time.Time) ProjectionActivity {
	if ctx == nil || db == nil || now.IsZero() || len(handlers) < 1 || len(handlers) > 5 {
		return ProjectionActivity{}
	}
	var predicates []string
	var args []any
	for i, handler := range handlers {
		if handler == "" || len(handler) > 195 {
			return ProjectionActivity{}
		}
		for _, previous := range handlers[:i] {
			if previous == handler {
				return ProjectionActivity{}
			}
		}
		predicates = append(predicates, "(r.handler = ? AND r.resource = ?)")
		args = append(args, handler, "core:"+handler)
	}
	var rows []struct {
		ID            string
		Token         string
		Worker        string
		Handler       string
		State         string
		AttemptRun    string
		AttemptWorker string
		AttemptState  string
		StartedAt     *time.Time
		ExpiresAt     *time.Time
	}
	query := db.WithContext(ctx).Table("job_runs AS r").
		Select("r.id, r.token, r.worker, r.handler, r.state, a.run_id AS attempt_run, a.worker AS attempt_worker, a.state AS attempt_state, a.started_at, a.expires_at").
		Joins("LEFT JOIN job_attempts AS a ON a.token = r.token").
		Where("r.owner = ? AND r.state IN ?", "system", []jobs.State{jobs.Running, jobs.CancelRequested, jobs.Unknown}).
		Where(strings.Join(predicates, " OR "), args...).
		Limit(len(handlers) + 1).Find(&rows)
	if query.Error != nil || len(rows) > len(handlers) {
		return ProjectionActivity{}
	}
	out := ProjectionActivity{Known: true, Active: len(rows)}
	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row.Handler] || row.State == string(jobs.Unknown) || row.Token == "" || row.Worker == "" || row.ID != row.AttemptRun || row.Worker != row.AttemptWorker || (row.AttemptState != string(jobs.Running) && row.AttemptState != string(jobs.CancelRequested)) || row.StartedAt == nil || row.ExpiresAt == nil || row.StartedAt.After(now) || !row.ExpiresAt.After(*row.StartedAt) {
			return ProjectionActivity{}
		}
		seen[row.Handler] = true
		out.Longest = max(out.Longest, now.Sub(*row.StartedAt))
	}
	return out
}
