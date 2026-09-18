package jobstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"gorm.io/gorm"
)

// Schedule reconciles one definition and atomically generates at most one
// intent for a planned slot. The next slot advances from planned time, never
// from completion time, so execution duration cannot drift the cadence.
func (s *Store) Schedule(ctx context.Context, d jobs.Definition) error {
	if d.Resource == jobs.MaintenanceResource {
		return jobs.ErrInvalid
	}
	if d.MaxAttempts == 0 {
		d.MaxAttempts = 1
	}
	if d.MaxAttempts > 1 && d.RetryBackoff == 0 {
		d.RetryBackoff = time.Second
	}
	if d.TimeZone == "" {
		d.TimeZone = "UTC"
	}
	if d.MisfirePolicy == "" {
		d.MisfirePolicy = jobs.MisfireFireOnce
	}
	d.DispatchLane = normalizedDispatchLane(d.Owner, d.ExecutionGroup, d.DispatchLane)
	if !d.FirstPlannedAt.IsZero() {
		d.FirstPlannedAt = d.FirstPlannedAt.UTC().Truncate(time.Millisecond)
	}
	if _, err := time.LoadLocation(d.TimeZone); err != nil {
		return jobs.ErrInvalid
	}
	if d.ID == "" || len(d.ID) > 320 || d.Owner == "" || d.Handler == "" || d.Interval < time.Millisecond || d.Timeout < time.Millisecond || d.Timeout > time.Hour || d.MaxAttempts < 1 || d.MaxAttempts > 10 || d.RetryBackoff < 0 || d.RetryBackoff > time.Hour || d.RetryBackoff%time.Millisecond != 0 || (d.MaxAttempts == 1 && d.RetryBackoff != 0) || len(d.Name) > 160 || len(d.Revision) > 160 || len(d.TimeZone) > 64 || len(d.DispatchLane) > 200 || (d.MisfirePolicy != jobs.MisfireFireOnce && d.MisfirePolicy != jobs.MisfireSkip) {
		return jobs.ErrInvalid
	}
	var current Schedule
	probe := s.db.WithContext(ctx).Where("id = ?", d.ID).Limit(1).Find(&current)
	if probe.Error != nil {
		return probe.Error
	}
	if probe.RowsAffected == 1 && scheduleDefinitionMatches(current, d) {
		if current.RunID == "" && (d.SuppressDispatch || current.NextAt.After(s.now())) {
			return nil
		}
		if current.RunID != "" {
			var r Record
			if err := s.db.WithContext(ctx).First(&r, "id = ?", current.RunID).Error; err != nil {
				return err
			}
			if r.State == string(jobs.Queued) || r.State == string(jobs.RetryWait) || r.State == string(jobs.Running) || r.State == string(jobs.CancelRequested) || (r.State == string(jobs.Unknown) && !d.ReconcileAfterLoss) {
				return nil
			}
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.lock(tx); err != nil {
			return err
		}
		now := s.now().UTC().Truncate(time.Millisecond)
		var row Schedule
		err := tx.First(&row, "id = ?", d.ID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			next := firstPlannedAt(d, now)
			row = Schedule{ID: d.ID, Owner: d.Owner, Name: d.Name, Handler: d.Handler, Resource: d.Resource, ExecutionGroup: d.ExecutionGroup, Revision: d.Revision, IntervalMS: d.Interval.Milliseconds(), TimeoutMS: d.Timeout.Milliseconds(), MaxAttempts: d.MaxAttempts, RetryBackoffMS: d.RetryBackoff.Milliseconds(), TimeZone: d.TimeZone, MisfirePolicy: string(d.MisfirePolicy), DispatchLane: d.DispatchLane, AnchorAt: timePointer(d.FirstPlannedAt), NextAt: next}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if row.Owner != d.Owner {
			return jobs.ErrConflict
		}
		if row.RunID != "" {
			var run Record
			if err := tx.First(&run, "id = ?", row.RunID).Error; err != nil {
				return err
			}
			if run.State == string(jobs.Unknown) && d.ReconcileAfterLoss {
				// The attempt retains its unknown outcome. This run is interrupted;
				// a later reconciler checks domain state rather than replaying it.
				if err := tx.Model(&Record{}).Where("id = ? AND state = ?", run.ID, jobs.Unknown).Update("state", jobs.Interrupted).Error; err != nil {
					return err
				}
				run.State = string(jobs.Interrupted)
			}
			if run.State == string(jobs.Running) || run.State == string(jobs.CancelRequested) || run.State == string(jobs.Queued) || run.State == string(jobs.RetryWait) || run.State == string(jobs.Unknown) {
				return nil
			}
			row.State = run.State
			row.LastState = run.State
			row.LastFinishedAt = run.FinishedAt
			row.RunID = ""

			if run.State == string(jobs.Yielded) && row.NextAt.After(now.Add(100*time.Millisecond)) {
				row.NextAt = now.Add(100 * time.Millisecond)
			}
		}
		definitionChanged := !scheduleDefinitionMatches(row, d)
		if definitionChanged {
			row.Revision = d.Revision
			row.NextAt = firstPlannedAt(d, now)
		}
		row.Name = d.Name
		row.Handler = d.Handler
		row.Resource = d.Resource
		row.ExecutionGroup = d.ExecutionGroup
		row.IntervalMS = d.Interval.Milliseconds()
		row.TimeoutMS = d.Timeout.Milliseconds()
		row.MaxAttempts = d.MaxAttempts
		row.RetryBackoffMS = d.RetryBackoff.Milliseconds()
		row.TimeZone = d.TimeZone
		row.MisfirePolicy = string(d.MisfirePolicy)
		row.DispatchLane = d.DispatchLane
		row.AnchorAt = timePointer(d.FirstPlannedAt)
		if !row.NextAt.After(now) && !d.SuppressDispatch {
			plannedAt, nextAt, missed, dispatch := resolveDueSlot(row.NextAt, now, d.Interval, d.MisfirePolicy)
			row.NextAt = nextAt
			row.MissedRuns += missed
			if !dispatch {
				return tx.Save(&row).Error
			}
			row.Sequence++
			child := &Store{db: tx, now: s.now}
			payload, _ := json.Marshal(map[string]string{"revision": d.Revision})
			run, err := child.Submit(ctx, jobs.Submission{Timeout: d.Timeout, MaxAttempts: d.MaxAttempts, RetryBackoff: d.RetryBackoff, Owner: d.Owner, Key: fmt.Sprintf("schedule:%x:%s", sha256.Sum256([]byte(d.ID)), plannedAt.Format(time.RFC3339Nano)), Handler: d.Handler, Resource: d.Resource, ExecutionGroup: d.ExecutionGroup, DispatchLane: d.DispatchLane, Payload: string(payload)})
			if err != nil {
				return err
			}
			if err := tx.Model(&Record{}).Where("id = ?", run.ID).Updates(map[string]any{"schedule_id": d.ID, "planned_at": plannedAt}).Error; err != nil {
				return err
			}
			row.RunID = run.ID
			row.State = string(jobs.Queued)
		}
		return tx.Save(&row).Error
	})
}

func (s *Store) Schedules(ctx context.Context) ([]jobs.ScheduleView, error) {
	var rows []Schedule
	if err := s.db.WithContext(ctx).Order("id").Limit(10000).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]jobs.ScheduleView, 0, len(rows))
	for _, r := range rows {
		first := time.Time{}
		if r.AnchorAt != nil {
			first = *r.AnchorAt
		}
		out = append(out, jobs.ScheduleView{Definition: jobs.Definition{ID: r.ID, Owner: r.Owner, Name: r.Name, Handler: r.Handler, Resource: r.Resource, ExecutionGroup: r.ExecutionGroup, Revision: r.Revision, Interval: time.Duration(r.IntervalMS) * time.Millisecond, Timeout: time.Duration(r.TimeoutMS) * time.Millisecond, MaxAttempts: r.MaxAttempts, RetryBackoff: time.Duration(r.RetryBackoffMS) * time.Millisecond, TimeZone: r.TimeZone, MisfirePolicy: jobs.MisfirePolicy(r.MisfirePolicy), FirstPlannedAt: first, DispatchLane: r.DispatchLane}, NextAt: r.NextAt, Runs: r.Runs, Failures: r.Failures, MissedRuns: r.MissedRuns, State: jobs.State(r.State), LastState: jobs.State(r.LastState), LastStartedAt: r.LastStartedAt, LastFinishedAt: r.LastFinishedAt})
	}
	return out, nil
}

func scheduleDefinitionMatches(row Schedule, d jobs.Definition) bool {
	return row.Owner == d.Owner && row.Name == d.Name && row.Handler == d.Handler && row.Resource == d.Resource && row.ExecutionGroup == d.ExecutionGroup && row.Revision == d.Revision && row.IntervalMS == d.Interval.Milliseconds() && row.TimeoutMS == d.Timeout.Milliseconds() && row.MaxAttempts == d.MaxAttempts && row.RetryBackoffMS == d.RetryBackoff.Milliseconds() && row.TimeZone == d.TimeZone && row.MisfirePolicy == string(d.MisfirePolicy) && row.DispatchLane == d.DispatchLane && sameTime(row.AnchorAt, d.FirstPlannedAt)
}

func sameTime(value *time.Time, expected time.Time) bool {
	if expected.IsZero() {
		return value == nil || value.IsZero()
	}
	return value != nil && value.Equal(expected)
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value
	return &copy
}

func firstPlannedAt(d jobs.Definition, now time.Time) time.Time {
	if !d.FirstPlannedAt.IsZero() {
		return d.FirstPlannedAt
	}
	if d.Immediate {
		return now
	}
	return now.Add(d.Interval)
}

func resolveDueSlot(next, now time.Time, interval time.Duration, policy jobs.MisfirePolicy) (plannedAt, nextAt time.Time, missed uint64, dispatch bool) {
	steps := uint64(now.Sub(next)/interval) + 1
	nextAt = next.Add(time.Duration(steps) * interval)
	if policy == jobs.MisfireSkip && steps > 1 {
		return time.Time{}, nextAt, steps, false
	}
	plannedAt = next.Add(time.Duration(steps-1) * interval)
	return plannedAt, nextAt, steps - 1, true
}
