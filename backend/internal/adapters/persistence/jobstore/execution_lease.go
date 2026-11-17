package jobstore

import (
	"context"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"gorm.io/gorm"
)

const workerLeaseTTL = 30 * time.Second

// Attempt.ExpiresAt remains the immutable execution deadline. Record.ExpiresAt
// is the renewable worker fence. Existing legacy claims keep both equal and
// expire naturally; startup never steals a foreign live claim.
func (s *Store) RenewLease(ctx context.Context, claim jobs.Claim) (expiry time.Time, err error) {
	if claim.Token == "" || claim.Worker == "" || claim.Run.ID == "" || claim.DeadlineAt.IsZero() {
		return expiry, jobs.ErrInvalid
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.lock(tx); err != nil {
			return err
		}
		now := s.now().UTC().Truncate(time.Millisecond)
		var run Record
		if err := tx.Where("id = ? AND token = ? AND worker = ? AND state = ? AND expires_at > ?", claim.Run.ID, claim.Token, claim.Worker, jobs.Running, now).Take(&run).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return jobs.ErrLeaseLost
			}
			return err
		}
		var attempt Attempt
		if err := tx.Where("token = ? AND run_id = ? AND worker = ? AND state = ? AND expires_at > ?", claim.Token, run.ID, claim.Worker, jobs.Running, now).Take(&attempt).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return jobs.ErrLeaseLost
			}
			return err
		}
		if !claim.DeadlineAt.Equal(attempt.ExpiresAt) {
			return jobs.ErrLeaseLost
		}
		expiry = minTime(now.Add(workerLeaseTTL), attempt.ExpiresAt)
		return tx.Model(&Record{}).Where("id = ? AND token = ?", run.ID, claim.Token).Update("expires_at", expiry).Error
	})
	return
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// Claim history can be large. Resolve a bounded set of active leases first,
// then update attempts by their primary-key tokens. Idle admission performs no
// writes or full-history scan against job_attempts.
func expireExecutionLeases(tx *gorm.DB, now time.Time) error {
	var expired []Record
	if err := tx.Select("id, token").Where("state IN ? AND expires_at <= ?", []jobs.State{jobs.Running, jobs.CancelRequested}, now).Order("id").Limit(128).Find(&expired).Error; err != nil {
		return err
	}
	if len(expired) == 0 {
		return nil
	}
	ids := make([]string, 0, len(expired))
	tokens := make([]string, 0, len(expired))
	for _, run := range expired {
		ids = append(ids, run.ID)
		if run.Token != "" {
			tokens = append(tokens, run.Token)
		}
	}
	if len(tokens) > 0 {
		if err := tx.Model(&Attempt{}).Where("token IN ? AND state IN ?", tokens, []jobs.State{jobs.Running, jobs.CancelRequested}).Updates(map[string]any{"state": jobs.Unknown, "finished_at": now}).Error; err != nil {
			return err
		}
	}
	if err := tx.Model(&Record{}).Where("id IN ? AND state IN ? AND expires_at <= ?", ids, []jobs.State{jobs.Running, jobs.CancelRequested}, now).Updates(map[string]any{"state": jobs.Unknown, "finished_at": now}).Error; err != nil {
		return err
	}
	return tx.Model(&Schedule{}).Where("state IN ? AND run_id IN ?", []jobs.State{jobs.Running, jobs.CancelRequested}, ids).Updates(map[string]any{"state": jobs.Unknown, "last_state": jobs.Unknown, "last_finished_at": now, "failures": gorm.Expr("failures + 1")}).Error
}
