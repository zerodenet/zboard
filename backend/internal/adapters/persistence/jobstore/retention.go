package jobstore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"gorm.io/gorm"
)

// PrunePeriodic removes bounded batches of terminal periodic history. Explicit
// submission keys remain durable, and uncertain or current runs are retained.
func (s *Store) PrunePeriodic(ctx context.Context, before time.Time) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.lock(tx); err != nil {
			return err
		}
		var ids []string
		if err := tx.Model(&Record{}).Where("schedule_id <> '' AND finished_at < ? AND state IN ?", before, []jobs.State{jobs.Succeeded, jobs.Failed, jobs.Yielded, jobs.Interrupted, jobs.Canceled}).Where("id NOT IN (?)", tx.Model(&Schedule{}).Select("run_id").Where("run_id <> ''")).Order("finished_at,id").Limit(1000).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := tx.Where("run_id IN ?", ids).Delete(&Attempt{}).Error; err != nil {
			return err
		}
		result := tx.Where("id IN ?", ids).Delete(&Record{})
		count = result.RowsAffected
		return result.Error
	})
	return count, err
}
