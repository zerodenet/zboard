package jobstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Store struct {
	db  *gorm.DB
	now func() time.Time
}

func New(db *gorm.DB) *Store {
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Store) Submit(ctx context.Context, in jobs.Submission) (jobs.Run, error) {
	if in.Resource == jobs.MaintenanceResource && in.Owner != "system" {
		return jobs.Run{}, jobs.ErrInvalid
	}
	if in.MaxAttempts == 0 {
		in.MaxAttempts = 1
	}
	if in.MaxAttempts > 1 && in.RetryBackoff == 0 {
		in.RetryBackoff = time.Second
	}
	in.DispatchLane = normalizedDispatchLane(in.Owner, in.ExecutionGroup, in.DispatchLane)
	if in.Timeout < 0 || in.Timeout > time.Hour || (in.Timeout != 0 && in.Timeout < time.Millisecond) || in.Timeout%time.Millisecond != 0 || in.MaxAttempts < 1 || in.MaxAttempts > 10 || in.RetryBackoff < 0 || in.RetryBackoff > time.Hour || in.RetryBackoff%time.Millisecond != 0 || (in.MaxAttempts == 1 && in.RetryBackoff != 0) || in.Owner == "" || len(in.Owner) > 200 || in.Key == "" || len(in.Key) > 160 || in.Handler == "" || len(in.Handler) > 320 || len(in.ExecutionGroup) > 160 || len(in.DispatchLane) > 200 || len(in.Resource) > 200 || len(in.Payload) > 65536 || !json.Valid([]byte(in.Payload)) {
		return jobs.Run{}, jobs.ErrInvalid
	}
	// Preserve zero NotBefore in the fingerprint so replaying an immediate
	// submission after restart still matches the first accepted request.
	b, _ := json.Marshal(struct {
		Timeout        time.Duration `json:",omitempty"`
		MaxAttempts    int           `json:",omitempty"`
		RetryBackoff   time.Duration `json:",omitempty"`
		Owner          string
		Key            string
		Handler        string
		Resource       string
		ExecutionGroup string
		Payload        string
		NotBefore      time.Time
	}{Timeout: in.Timeout, MaxAttempts: in.MaxAttempts, RetryBackoff: in.RetryBackoff, Owner: in.Owner, Key: in.Key, Handler: in.Handler, Resource: in.Resource, ExecutionGroup: in.ExecutionGroup, Payload: in.Payload, NotBefore: in.NotBefore})
	h := sha256.Sum256(b)
	fingerprint := hex.EncodeToString(h[:])
	now := s.now().UTC().Truncate(time.Millisecond)
	if in.NotBefore.IsZero() {
		in.NotBefore = now
	}
	r := Record{TimeoutMS: in.Timeout.Milliseconds(), MaxAttempts: in.MaxAttempts, RetryBackoffMS: in.RetryBackoff.Milliseconds(), ID: uuid.NewString(), Owner: in.Owner, Key: in.Key, Handler: in.Handler, Resource: in.Resource, ExecutionGroup: in.ExecutionGroup, DispatchLane: in.DispatchLane, Payload: in.Payload, Fingerprint: fingerprint, State: string(jobs.Queued), NotBefore: in.NotBefore, CreatedAt: now}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.lock(tx); err != nil {
			return err
		}
		var existing Record
		found := tx.Where("owner = ? AND `key` = ?", in.Owner, in.Key).Limit(1).Find(&existing)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected == 1 {
			r = existing
			if existing.Fingerprint != fingerprint {
				return jobs.ErrConflict
			}
			return nil
		}
		if err := checkPendingAdmission(tx, in.Owner, in.Resource); err != nil {
			return err
		}
		if in.ExecutionGroup != "" {
			var group ExecutionGroup
			result := tx.Where("id = ? AND capacity > 0", in.ExecutionGroup).Limit(1).Find(&group)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return jobs.ErrInvalid
			}
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&r).Error; err != nil {
			return err
		}
		r = Record{}
		if err := tx.Where("owner = ? AND `key` = ?", in.Owner, in.Key).Take(&r).Error; err != nil {
			return err
		}
		if r.Fingerprint != fingerprint {
			return jobs.ErrConflict
		}
		return nil
	})
	if err != nil {
		return jobs.Run{}, err
	}
	return view(r), nil
}

func normalizedDispatchLane(owner, group, lane string) string {
	lane = strings.TrimSpace(lane)
	if lane != "" {
		return lane
	}
	if strings.HasPrefix(owner, "plugin:") {
		return owner
	}
	if group != "" {
		return "group:" + group
	}
	return "core"
}

func dispatchWeight(lane string) uint64 {
	switch {
	case lane == "core":
		return 4
	case strings.HasPrefix(lane, "group:"):
		return 2
	default:
		return 1
	}
}

// A write to the one budget row serializes admission on both MySQL and SQLite.
// Capacity is persisted once by the schema/bootstrap owner, not per worker.
func (s *Store) lock(tx *gorm.DB) (Budget, error) {
	r := tx.Model(&Budget{}).Where("id = 1").UpdateColumn("revision", gorm.Expr("revision + 1"))
	if r.Error != nil {
		return Budget{}, r.Error
	}
	if r.RowsAffected != 1 {
		return Budget{}, jobs.ErrInvalid
	}
	var b Budget
	// Use a current read: a plain SELECT would establish MySQL's repeatable
	// read snapshot before callers acquire their domain row locks.
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&b, 1).Error
	if err == nil && b.Capacity <= 0 {
		err = jobs.ErrInvalid
	}
	return b, err
}

func (s *Store) Claim(ctx context.Context, worker string, handlers []string, timeout time.Duration) (out jobs.Claim, err error) {
	if worker == "" || len(worker) > 160 || len(handlers) == 0 || timeout <= 0 || timeout > time.Hour {
		return out, jobs.ErrInvalid
	}
	var candidate struct{ Present int }
	now := s.now().UTC().Truncate(time.Millisecond)
	probe := s.db.WithContext(ctx).Model(&Record{}).Select("1 AS present").Where("(state IN ? AND not_before <= ? AND handler IN ?) OR (state IN ? AND expires_at <= ?)", []jobs.State{jobs.Queued, jobs.RetryWait}, now, handlers, []jobs.State{jobs.Running, jobs.CancelRequested}, now).Limit(1).Scan(&candidate)
	if probe.Error != nil {
		return out, probe.Error
	}
	if candidate.Present == 0 {
		return out, jobs.ErrEmpty
	}
	empty := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		budget, err := s.lock(tx)
		if err != nil {
			return err
		}
		now := s.now().UTC().Truncate(time.Millisecond)
		// Persist uncertainty even when no next job can be claimed.
		if err := tx.Model(&Attempt{}).Where("state IN ? AND expires_at <= ?", []jobs.State{jobs.Running, jobs.CancelRequested}, now).Updates(map[string]any{"state": jobs.Unknown, "finished_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Record{}).Where("state IN ? AND expires_at <= ?", []jobs.State{jobs.Running, jobs.CancelRequested}, now).Updates(map[string]any{"state": jobs.Unknown, "finished_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Schedule{}).Where("state IN ? AND run_id IN (?)", []jobs.State{jobs.Running, jobs.CancelRequested}, tx.Model(&Record{}).Select("id").Where("state = ?", jobs.Unknown)).Updates(map[string]any{"state": jobs.Unknown, "last_state": jobs.Unknown, "last_finished_at": now, "failures": gorm.Expr("failures + 1")}).Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&Record{}).Where("state IN ?", []jobs.State{jobs.Running, jobs.CancelRequested}).Count(&n).Error; err != nil {
			return err
		}
		if n >= int64(budget.Capacity) {
			empty = true
			return nil
		}
		var maintenance int64
		if err := tx.Model(&Record{}).Where("resource = ? AND state IN ?", jobs.MaintenanceResource, []jobs.State{jobs.Queued, jobs.RetryWait, jobs.Running, jobs.CancelRequested, jobs.Unknown}).Count(&maintenance).Error; err != nil {
			return err
		}
		if maintenance > 0 {
			var uncertain int64
			if err := tx.Model(&Record{}).Where("state = ?", jobs.Unknown).Count(&uncertain).Error; err != nil {
				return err
			}
			if n > 0 || uncertain > 0 {
				empty = true
				return nil
			}
		}
		// No candidate limit before resource filtering: a busy resource must not
		// starve unrelated work behind it. Eligible lanes are selected with
		// persisted weighted virtual time before FIFO is applied within a lane.
		eligible := tx.Model(&Record{}).
			Where("? = 0 OR (resource = ? AND owner = ?)", maintenance, jobs.MaintenanceResource, "system").
			Where("state IN ? AND not_before <= ? AND handler IN ?", []jobs.State{jobs.Queued, jobs.RetryWait}, now, handlers).
			Where("resource = '' OR resource NOT IN (?)", tx.Model(&Record{}).Select("resource").Where("state IN ? AND resource <> ''", []jobs.State{jobs.Running, jobs.CancelRequested, jobs.Unknown})).
			Where("execution_group = '' OR (SELECT COUNT(*) FROM job_runs AS active_group WHERE active_group.execution_group = job_runs.execution_group AND active_group.state IN ('running','cancel_requested')) < (SELECT capacity FROM job_execution_groups WHERE id = job_runs.execution_group)")
		lane, start, finish, err := selectDispatchLane(eligible, budget.DispatchVirtualTime)
		if err != nil {
			return err
		}
		if lane == "" {
			empty = true
			return nil
		}
		var row Record
		q := eligible.Where("dispatch_lane = ?", lane).Order("not_before ASC, created_at ASC, id ASC").Limit(1).Find(&row)
		if q.Error == nil && q.RowsAffected == 0 {
			empty = true
			return nil
		}
		if q.Error != nil {
			return q.Error
		}
		if row.TimeoutMS > 0 {
			timeout = time.Duration(row.TimeoutMS) * time.Millisecond
			if timeout <= 0 || timeout > time.Hour {
				return jobs.ErrInvalid
			}
		} else if row.ScheduleID != "" {
			var schedule Schedule
			if err := tx.First(&schedule, "id = ?", row.ScheduleID).Error; err != nil {
				return err
			}
			timeout = time.Duration(schedule.TimeoutMS) * time.Millisecond
			if timeout <= 0 || timeout > time.Hour {
				return jobs.ErrInvalid
			}
		}
		token := uuid.NewString()
		expiry := now.Add(timeout)
		attempt := row.AttemptCount + 1
		res := tx.Model(&Record{}).Where("id = ? AND state IN ? AND attempt_count = ?", row.ID, []jobs.State{jobs.Queued, jobs.RetryWait}, row.AttemptCount).Updates(map[string]any{"state": jobs.Running, "token": token, "worker": worker, "expires_at": expiry, "attempt_count": attempt})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return jobs.ErrConflict
		}
		if err := tx.Create(&Attempt{Token: token, RunID: row.ID, Number: attempt, Worker: worker, State: string(jobs.Running), StartedAt: now, ExpiresAt: expiry}).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "lane"}}, DoUpdates: clause.Assignments(map[string]any{"virtual_finish": finish})}).Create(&DispatchLane{Lane: lane, VirtualFinish: finish}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Budget{}).Where("id = 1").Update("dispatch_virtual_time", start).Error; err != nil {
			return err
		}
		if row.ScheduleID != "" {
			if err := tx.Model(&Schedule{}).Where("id = ? AND run_id = ?", row.ScheduleID, row.ID).Updates(map[string]any{"state": jobs.Running, "last_started_at": now, "runs": gorm.Expr("runs + 1")}).Error; err != nil {
				return err
			}
		}
		row.State = string(jobs.Running)
		row.AttemptCount = attempt
		out = jobs.Claim{Run: view(row), Token: token, Worker: worker, ExpiresAt: expiry}
		return nil
	})
	if err == nil && empty {
		err = jobs.ErrEmpty
	}
	return
}

type eligibleLane struct {
	Lane          string    `gorm:"column:dispatch_lane"`
	Oldest        time.Time `gorm:"column:oldest"`
	VirtualFinish uint64
	Effective     uint64 `gorm:"-"`
}

func selectDispatchLane(eligible *gorm.DB, virtualTime uint64) (lane string, start, finish uint64, err error) {
	var lanes []eligibleLane
	if err = eligible.Session(&gorm.Session{}).Select("dispatch_lane, MIN(not_before) AS oldest").Group("dispatch_lane").Scan(&lanes).Error; err != nil || len(lanes) == 0 {
		return "", 0, 0, err
	}
	names := make([]string, 0, len(lanes))
	for _, candidate := range lanes {
		names = append(names, candidate.Lane)
	}
	var states []DispatchLane
	if err = eligible.Session(&gorm.Session{NewDB: true}).Model(&DispatchLane{}).Where("lane IN ?", names).Find(&states).Error; err != nil {
		return "", 0, 0, err
	}
	stored := make(map[string]uint64, len(states))
	for _, state := range states {
		stored[state.Lane] = state.VirtualFinish
	}
	for i := range lanes {
		lanes[i].VirtualFinish = stored[lanes[i].Lane]
		lanes[i].Effective = max(lanes[i].VirtualFinish, virtualTime)
	}
	sort.Slice(lanes, func(i, j int) bool {
		if lanes[i].Effective != lanes[j].Effective {
			return lanes[i].Effective < lanes[j].Effective
		}
		if !lanes[i].Oldest.Equal(lanes[j].Oldest) {
			return lanes[i].Oldest.Before(lanes[j].Oldest)
		}
		return lanes[i].Lane < lanes[j].Lane
	})
	selected := lanes[0]
	quantum := (uint64(1024) + dispatchWeight(selected.Lane) - 1) / dispatchWeight(selected.Lane)
	return selected.Lane, selected.Effective, selected.Effective + quantum, nil
}

func (s *Store) Finish(ctx context.Context, c jobs.Claim, state jobs.State) error {
	if state != jobs.Succeeded && state != jobs.Failed && state != jobs.Unknown && state != jobs.Yielded && state != jobs.RetryWait && state != jobs.Canceled {
		return jobs.ErrInvalid
	}
	lost := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.lock(tx); err != nil {
			return err
		}
		now := s.now().UTC().Truncate(time.Millisecond)
		var r Record
		if err := tx.Where("id = ? AND token = ? AND worker = ? AND state IN ?", c.Run.ID, c.Token, c.Worker, []jobs.State{jobs.Running, jobs.CancelRequested}).Take(&r).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return jobs.ErrLeaseLost
			}
			return err
		}
		cancelRequested := r.State == string(jobs.CancelRequested)
		if r.ExpiresAt == nil || !r.ExpiresAt.After(now) {
			state = jobs.Unknown
			lost = true
		}
		if cancelRequested {
			switch state {
			case jobs.Succeeded, jobs.Canceled:
			default:
				state = jobs.Unknown
			}
		} else if state == jobs.Canceled {
			state = jobs.Unknown
		}
		updates := map[string]any{"state": state, "finished_at": now}
		if state == jobs.RetryWait {
			if r.AttemptCount >= r.MaxAttempts {
				state = jobs.Failed
				updates["state"] = state
			} else {
				updates["not_before"] = now.Add(retryDelay(r.RetryBackoffMS, r.AttemptCount))
				updates["finished_at"] = nil
				updates["token"] = ""
				updates["worker"] = ""
				updates["expires_at"] = nil
			}
		}
		if err := tx.Model(&Record{}).Where("id = ?", r.ID).Updates(updates).Error; err != nil {
			return err
		}
		if r.ScheduleID != "" {
			updates := map[string]any{"state": state, "last_state": state, "last_finished_at": now}
			if state == jobs.Failed || state == jobs.Unknown {
				updates["failures"] = gorm.Expr("failures + 1")
			}
			if err := tx.Model(&Schedule{}).Where("id = ? AND run_id = ?", r.ScheduleID, r.ID).Updates(updates).Error; err != nil {
				return err
			}
		}
		return tx.Model(&Attempt{}).Where("token = ?", c.Token).Updates(map[string]any{"state": state, "finished_at": now}).Error
	})
	if err == nil && lost {
		return jobs.ErrLeaseLost
	}
	return err
}

func retryDelay(backoffMS int64, attempt int) time.Duration {
	delay := time.Duration(backoffMS) * time.Millisecond
	if delay <= 0 {
		delay = time.Second
	}
	for i := 1; i < attempt && delay < time.Hour; i++ {
		delay *= 2
	}
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}

func (s *Store) List(ctx context.Context, owner string, limit, offset int) ([]jobs.Run, error) {
	if owner == "" || limit < 1 || limit > 100 || offset < 0 {
		return nil, jobs.ErrInvalid
	}
	var rows []Record
	err := s.db.WithContext(ctx).Where("owner = ?", owner).Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&rows).Error
	out := make([]jobs.Run, 0, len(rows))
	for _, r := range rows {
		out = append(out, view(r))
	}
	return out, err
}
func view(r Record) jobs.Run {
	return jobs.Run{ID: r.ID, Submission: jobs.Submission{Timeout: time.Duration(r.TimeoutMS) * time.Millisecond, MaxAttempts: r.MaxAttempts, RetryBackoff: time.Duration(r.RetryBackoffMS) * time.Millisecond, Owner: r.Owner, Key: r.Key, Handler: r.Handler, Resource: r.Resource, ExecutionGroup: r.ExecutionGroup, DispatchLane: r.DispatchLane, Payload: r.Payload, NotBefore: r.NotBefore}, State: jobs.State(r.State), Attempt: r.AttemptCount, PlannedAt: r.PlannedAt, CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt}
}
