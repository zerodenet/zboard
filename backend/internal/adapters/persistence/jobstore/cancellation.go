package jobstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Store) RecordCancellation(ctx context.Context, actor jobs.Reviewer, in jobs.Cancellation) (jobs.State, error) {
	if actor.AccountID == 0 || in.RunID == "" || strings.TrimSpace(in.Reason) == "" {
		return "", jobs.ErrInvalid
	}
	state := jobs.State("")
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.lock(tx); err != nil {
			return err
		}
		var user model.User
		query := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND status = ? AND is_admin = ?", actor.AccountID, "active", true).Limit(1).Find(&user)
		if query.Error != nil {
			return query.Error
		}
		if query.RowsAffected != 1 {
			return jobs.ErrPermission
		}
		var run Record
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&run, "id = ?", in.RunID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return jobs.ErrConflict
			}
			return err
		}
		if run.State == string(jobs.CancelRequested) {
			state = jobs.CancelRequested
			return nil
		}
		now := s.now().UTC().Truncate(time.Millisecond)
		updates := map[string]any{}
		switch jobs.State(run.State) {
		case jobs.Queued, jobs.RetryWait:
			state = jobs.Canceled
			updates["state"], updates["finished_at"] = state, now
		case jobs.Running:
			state = jobs.CancelRequested
			updates["state"] = state
		default:
			return jobs.ErrConflict
		}
		if err := tx.Model(&Record{}).Where("id = ? AND state = ?", run.ID, run.State).Updates(updates).Error; err != nil {
			return err
		}
		if state == jobs.CancelRequested && run.Token != "" {
			if err := tx.Model(&Attempt{}).Where("token = ? AND state = ?", run.Token, jobs.Running).Update("state", state).Error; err != nil {
				return err
			}
		}
		if err := updateCanceledSchedule(tx, run, state, now); err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "job.cancel", Target: "job:" + run.ID, Detail: strings.TrimSpace(in.Reason) + ": " + run.State + "->" + string(state)}).Error
	})
	return state, err
}

func updateCanceledSchedule(tx *gorm.DB, run Record, state jobs.State, now time.Time) error {
	if run.ScheduleID == "" {
		return nil
	}
	updates := map[string]any{"state": state}
	if state == jobs.Canceled {
		updates["last_state"] = state
		updates["last_finished_at"] = now
	}
	return tx.Model(&Schedule{}).Where("id = ? AND run_id = ?", run.ScheduleID, run.ID).Updates(updates).Error
}

func (s *Store) CancellationRequested(ctx context.Context, claim jobs.Claim) (bool, error) {
	if claim.Run.ID == "" || claim.Token == "" || claim.Worker == "" {
		return false, jobs.ErrInvalid
	}
	var count int64
	err := s.db.WithContext(ctx).Model(&Record{}).Where("id = ? AND token = ? AND worker = ? AND state = ?", claim.Run.ID, claim.Token, claim.Worker, jobs.CancelRequested).Count(&count).Error
	return count == 1, err
}
