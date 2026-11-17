package meteringstore

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Exact receive-time diagnostics expire independently of minute history. Only
// verified projected rows retire; deleting raw/points must not erase chart data.
// No financial ledger, cumulative cursor or original accounting proof is touched.
func (s ObservationRetention) pruneProjectedFlowStarts(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	return s.pruneProjectedFlowStartsAt(ctx, cutoff, cutoff.Add(metering.ObservationRetentionHorizon), limit)
}

func (s ObservationRetention) pruneProjectedFlowStartsAt(ctx context.Context, cutoff, now time.Time, limit int) (int64, error) {
	if limit < 1 || limit > metering.ObservationRetentionBatchLimit {
		return 0, errors.New("invalid flow retention batch")
	}
	var ids []uint64
	if err := s.DB.WithContext(ctx).Model(&model.FlowStartStatisticsPoint{}).Where("received_at<? AND review_at<=?", cutoff, now).Order("review_at,source_id").Limit(limit).Pluck("source_id", &ids).Error; err != nil {
		return 0, err
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var deleted int64
	var failures []error
	for start := 0; start < len(ids); start += 32 {
		chunk := ids[start:min(start+32, len(ids))]
		var held []error
		var done int64
		err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// Prevent raw-of-later-batch -> earlier-minute ABBA with a multi-source
			// projector. Source writers never lock this auxiliary checkpoint.
			var state model.FlowStartStatisticsState
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&state, "id=?", 1).Error; err != nil {
				return err
			}
			var sources []FlowStartRecord
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(flowStatisticsSourceColumns).Where("id IN ?", chunk).Order("id").Find(&sources).Error; err != nil {
				return err
			}
			var pending []model.FlowStartStatisticsPending
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_id IN ?", chunk).Order("source_id").Find(&pending).Error; err != nil {
				return err
			}
			busy := map[uint64]bool{}
			for _, p := range pending {
				busy[p.SourceID] = true
			}
			var points []model.FlowStartStatisticsPoint
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(flowPointColumns).Where("source_id IN ?", chunk).Order("source_id").Find(&points).Error; err != nil {
				return err
			}
			byID := map[uint64]model.FlowStartStatisticsPoint{}
			for _, p := range points {
				byID[p.SourceID] = p
			}

			foundSources := map[uint64]bool{}
			for _, source := range sources {
				foundSources[source.ID] = true
			}
			for _, point := range points {
				if !foundSources[point.SourceID] {
					held = append(held, errors.New("old flow raw source missing; retained statistical evidence"))
					if err := tx.Model(&model.FlowStartStatisticsPoint{}).Where("source_id=?", point.SourceID).Update("review_at", now.Add(time.Hour)).Error; err != nil {
						return err
					}
				}
			}
			var clear []uint64
			for _, r := range sources {
				if !r.ReceivedAt.Before(cutoff) {
					continue
				}
				if busy[r.ID] || r.ID <= state.LegacyUpperID && r.ID > state.LegacyAfterID {
					if err := tx.Model(&model.FlowStartStatisticsPoint{}).Where("source_id = ?", r.ID).Update("review_at", now.Add(time.Minute)).Error; err != nil {
						return err
					}
					continue
				}
				p, ok := byID[r.ID]
				if !ok {
					continue
				}
				if !flowStatisticsPointMatches(p, r) {
					held = append(held, errors.New("old flow statistics source/receipt changed; retained original"))
					if err := tx.Model(&model.FlowStartStatisticsPoint{}).Where("source_id=?", r.ID).Update("review_at", now.Add(time.Hour)).Error; err != nil {
						return err
					}
					continue
				}
				clear = append(clear, r.ID)
			}
			if len(clear) == 0 {
				return nil
			}
			for _, record := range []any{&model.FlowStartStatisticsPoint{}, &FlowStartRecord{}} {
				column := "source_id"
				if _, ok := record.(*FlowStartRecord); ok {
					column = "id"
				}
				q := tx.Where(column+" IN ?", clear).Delete(record)
				if q.Error != nil {
					return q.Error
				}
				if q.RowsAffected != int64(len(clear)) {
					return errors.New("flow retirement source/point count changed")
				}
			}
			done = int64(len(clear))
			return nil
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			return deleted, errors.Join(append(failures, err)...)
		}
		deleted += done
		failures = append(failures, held...)
	}
	return deleted, errors.Join(failures...)
}
