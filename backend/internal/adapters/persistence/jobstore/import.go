package jobstore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"gorm.io/gorm"
)

// ImportUnknown attaches an existing, possibly executed intent to the common
// ledger. It never exposes a queued state or invents an execution attempt.
// Existing common-ledger intents, including queued/running ones, are untouched.
func (s *Store) ImportUnknown(ctx context.Context, in jobs.Submission, created time.Time) (id string, inserted bool, err error) {
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.lock(tx); err != nil {
			return err
		}
		var current Record
		q := tx.Where("owner = ? AND `key` = ?", in.Owner, in.Key).Limit(1).Find(&current)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 0 {
			id = current.ID
			return nil
		}
		run, err := New(tx).Submit(ctx, in)
		if err != nil {
			return err
		}
		id = run.ID
		updates := map[string]any{"state": jobs.Unknown}
		if !created.IsZero() {
			updates["created_at"] = created.UTC().Truncate(time.Millisecond)
		}
		if err := tx.Model(&Record{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}
		inserted = true
		return nil
	})
	return
}
