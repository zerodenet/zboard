package jobstore

import (
	"context"
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

func InspectProjectionActivity(ctx context.Context, db *gorm.DB, handlers [2]string, now time.Time) ProjectionActivity {
	if handlers[0] == "" || handlers[1] == "" || handlers[0] == handlers[1] {
		return ProjectionActivity{}
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
		Where("(r.handler = ? AND r.resource = ?) OR (r.handler = ? AND r.resource = ?)", handlers[0], "core:"+handlers[0], handlers[1], "core:"+handlers[1]).
		Limit(3).Find(&rows)
	if query.Error != nil || len(rows) > 2 {
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
