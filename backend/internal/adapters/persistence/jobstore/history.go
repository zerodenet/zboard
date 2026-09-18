package jobstore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CancelHandler is the lifecycle cancellation path used when a registered
// handler is disabled or replaced. Pending runs terminate immediately; active
// leases retain their fencing token while transitioning to cancel_requested.
func (s *Store) CancelHandler(ctx context.Context, handler string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.lock(tx); err != nil {
			return err
		}
		now := s.now().UTC().Truncate(time.Millisecond)
		var rows []Record
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("handler = ? AND state IN ?", handler, []jobs.State{jobs.Queued, jobs.RetryWait, jobs.Running, jobs.CancelRequested}).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			state := jobs.Canceled
			updates := map[string]any{"state": state, "finished_at": now}
			if row.State == string(jobs.Running) || row.State == string(jobs.CancelRequested) {
				state = jobs.CancelRequested
				updates = map[string]any{"state": state}
				if row.Token != "" {
					if err := tx.Model(&Attempt{}).Where("token = ? AND state IN ?", row.Token, []jobs.State{jobs.Running, jobs.CancelRequested}).Update("state", state).Error; err != nil {
						return err
					}
				}
			}
			if err := tx.Model(&Record{}).Where("id = ? AND state = ?", row.ID, row.State).Updates(updates).Error; err != nil {
				return err
			}
			if err := updateCanceledSchedule(tx, row, state, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// Resolve records an operator-verified outcome, never a retry. The caller must
// authorize the operator and commit its audit record in the same transaction.
func (s *Store) Resolve(ctx context.Context, id string, state jobs.State) error {
	if state != jobs.Succeeded && state != jobs.Failed {
		return jobs.ErrInvalid
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.lock(tx); err != nil {
			return err
		}
		now := s.now().UTC().Truncate(time.Millisecond)
		res := tx.Model(&Record{}).Where("id = ? AND state = ?", id, jobs.Unknown).Updates(map[string]any{"state": state, "finished_at": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return jobs.ErrConflict
		}
		return nil
	})
}

func (s *Store) History(ctx context.Context, limit, offset int) ([]jobs.Run, int64, error) {
	if limit < 1 || limit > 50 || offset < 0 || offset > 1000000 {
		return nil, 0, jobs.ErrInvalid
	}
	var rows []Record
	var total int64
	q := s.db.WithContext(ctx).Model(&Record{})
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("created_at DESC,id DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]jobs.Run, 0, len(rows))
	for _, r := range rows {
		out = append(out, view(r))
	}
	return out, total, nil
}

func (s *Store) Attempts(ctx context.Context, runID string, limit, offset int) ([]jobs.AttemptView, int64, error) {
	if runID == "" || limit < 1 || limit > 50 || offset < 0 {
		return nil, 0, jobs.ErrInvalid
	}
	query := s.db.WithContext(ctx).Model(&Attempt{}).Where("run_id = ?", runID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Attempt
	if err := query.Order("attempt_number DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]jobs.AttemptView, 0, len(rows))
	for _, row := range rows {
		out = append(out, jobs.AttemptView{Number: row.Number, Worker: row.Worker, State: jobs.State(row.State), StartedAt: row.StartedAt, ExpiresAt: row.ExpiresAt, FinishedAt: row.FinishedAt})
	}
	return out, total, nil
}
