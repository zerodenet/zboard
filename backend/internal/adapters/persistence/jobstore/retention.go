package jobstore

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

const periodicRetentionBatch = 1000

// PrunePeriodic keeps the compatibility entrypoint; policy callers use separate
// outcome cutoffs. Interrupted/unknown evidence is never age-pruned.
func (s *Store) PrunePeriodic(ctx context.Context, before time.Time) (int64, error) {
	return s.prunePeriodicStates(ctx, before, []jobs.State{jobs.Succeeded, jobs.Yielded, jobs.Failed, jobs.Canceled})
}
func (s *Store) PrunePeriodicOutcome(ctx context.Context, before time.Time, successful bool) (int64, error) {
	states := []jobs.State{jobs.Failed, jobs.Canceled}
	if successful {
		states = []jobs.State{jobs.Succeeded, jobs.Yielded}
	}
	return s.prunePeriodicStates(ctx, before, states)
}
func periodicCandidates(db *gorm.DB, before time.Time, state jobs.State) *gorm.DB {
	return db.Model(&Record{}).Where("schedule_id <> '' AND finished_at < ? AND state = ?", before, state).
		Where("id NOT IN (?)", db.Model(&Schedule{}).Select("run_id").Where("run_id <> ''"))
}
func (s *Store) prunePeriodicStates(ctx context.Context, before time.Time, states []jobs.State) (int64, error) {
	var deleted int64
	for _, state := range states {
		if deleted >= periodicRetentionBatch {
			break
		}
		var candidates []string
		// Discover via (state,finished_at,id) before opening a write transaction.
		if err := periodicCandidates(s.db.WithContext(ctx), before, state).Order("finished_at,id").Limit(periodicRetentionBatch-int(deleted)).Pluck("id", &candidates).Error; err != nil {
			return deleted, err
		}
		if len(candidates) == 0 {
			continue
		}
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var ids []string
			if err := periodicCandidates(tx, before, state).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", candidates).Order("id").Pluck("id", &ids).Error; err != nil {
				return err
			}
			if len(ids) == 0 {
				return nil
			}
			var tokens []string
			if err := tx.Model(&Attempt{}).Where("run_id IN ?", ids).Order("run_id,attempt_number").Limit(periodicRetentionBatch).Pluck("token", &tokens).Error; err != nil {
				return err
			}
			if len(tokens) > 0 {
				if err := tx.Where("token IN ?", tokens).Delete(&Attempt{}).Error; err != nil {
					return err
				}
			}
			res := tx.Where("id IN ?", ids).Where("NOT EXISTS (?)", tx.Model(&Attempt{}).Select("1").Where("job_attempts.run_id = job_runs.id")).Delete(&Record{})
			if res.Error != nil {
				return res.Error
			}
			deleted += res.RowsAffected
			return nil
		})
		if err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

func (s *Store) HasPrunablePeriodic(ctx context.Context, before time.Time, successful bool) (bool, error) {
	states := []jobs.State{jobs.Failed, jobs.Canceled}
	if successful {
		states = []jobs.State{jobs.Succeeded, jobs.Yielded}
	}
	for _, state := range states {
		var ids []string
		err := periodicCandidates(s.db.WithContext(ctx), before, state).Order("finished_at,id").Limit(1).Pluck("id", &ids).Error
		if err != nil {
			return false, err
		}
		if len(ids) > 0 {
			return true, nil
		}
	}
	return false, nil
}
