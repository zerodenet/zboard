package meteringstore

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ObservationRetention struct{ DB *gorm.DB }

func (s ObservationRetention) prune(ctx context.Context, model any, column string, cutoff time.Time, limit int) (int64, error) {
	if limit <= 0 || limit > metering.ObservationRetentionBatchLimit {
		return 0, fmt.Errorf("observation retention batch limit must be between 1 and %d", metering.ObservationRetentionBatchLimit)
	}
	// Discover outside the write transaction, without a locking time-range read.
	// LIMIT bounds candidates, not scanned rows. Startup migration 0056 supplies
	// the global evaluation time/PK index; flow starts use their receive index.
	var candidates []uint64
	if err := s.DB.WithContext(ctx).Model(model).Where(column+" < ?", cutoff).Order(column+" ASC, id ASC").Limit(limit).Pluck("id", &candidates).Error; err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		return 0, nil
	}
	// Discover in time-index order, then acquire row locks in primary-key order.
	sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
	var deleted int64
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current []struct {
			ID          uint64
			RetentionAt time.Time
		}
		// Point reads lock only discovered primary keys. READ COMMITTED avoids
		// missing-ID gap locks if another worker has already deleted them.
		if err := tx.Model(model).Select("id, "+column+" AS retention_at").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", candidates).Order("id ASC").Find(&current).Error; err != nil {
			return err
		}
		ids := make([]uint64, 0, len(current))
		for _, row := range current {
			// Revalidate the horizon under the row lock; unlocked candidates may
			// have been refreshed or removed since discovery.
			if row.RetentionAt.Before(cutoff) {
				ids = append(ids, row.ID)
			}
		}
		if len(ids) == 0 {
			return nil
		}
		result := tx.Where("id IN ?", ids).Delete(model)
		deleted = result.RowsAffected
		return result.Error
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}
