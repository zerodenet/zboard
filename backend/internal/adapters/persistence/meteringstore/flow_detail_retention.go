package meteringstore

import (
	"context"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const FlowStartDetailHorizon = 24 * time.Hour
const FlowStartFutureAllowance = 5 * time.Minute

type FlowDetailRetention struct {
	DB  *gorm.DB
	Now func() time.Time
}

func (s FlowDetailRetention) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s FlowDetailRetention) HasPending(ctx context.Context) (bool, error) {
	var state model.FlowDetailRetentionState
	err := s.DB.WithContext(ctx).First(&state, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	if err != nil || state.AfterID < state.UpperID {
		return err == nil, err
	}
	now := s.now()
	var point model.FlowStartStatisticsPoint
	r := s.DB.WithContext(ctx).Select("source_id").Where("review_at <= ? AND received_at < ?", now, now.Add(-FlowStartDetailHorizon)).Order("review_at,source_id").Limit(1).Find(&point)
	if r.Error != nil || r.RowsAffected > 0 {
		return r.RowsAffected > 0, r.Error
	}
	var projection model.FlowStartStatisticsState
	if err := s.DB.WithContext(ctx).First(&projection, 1).Error; err != nil {
		return false, err
	}
	if projection.LegacyAfterID != projection.LegacyUpperID || projection.LegacyProjectedRows != projection.LegacyExpectedRows {
		return false, nil
	}
	var minute model.FlowStartStatisticsMinute
	r = s.DB.WithContext(ctx).Select("minute_at").Where("minute_at < ?", now.Add(-metering.ObservationRetentionHorizon).Truncate(time.Minute)).Order("minute_at").Limit(1).Find(&minute)
	if r.Error != nil || r.RowsAffected > 0 {
		return r.RowsAffected > 0, r.Error
	}
	r = s.DB.WithContext(ctx).Select("minute_at").Where("minute_at > ?", now.Add(FlowStartFutureAllowance).Truncate(time.Minute)).Order("minute_at").Limit(1).Find(&minute)
	return r.RowsAffected > 0, r.Error
}

func (s FlowDetailRetention) Drain(ctx context.Context, limit int) (int, error) {
	if ctx == nil || s.DB == nil || limit < 1 || limit > 200 {
		return 0, errors.New("flow detail retention requires 1..200 rows")
	}
	n, _, err := s.convertDeadlines(ctx, limit)
	if err != nil {
		return n, err
	}
	now := s.now()
	removed, rawErr := (ObservationRetention{DB: s.DB}).pruneProjectedFlowStartsAt(ctx, now.Add(-FlowStartDetailHorizon), now, limit)
	// Minute history has independent expiry; a corrupt diagnostic must not starve it.
	expired, minuteErr := s.expireMinutes(ctx, now.Add(-metering.ObservationRetentionHorizon).Truncate(time.Minute), limit)
	return n + int(removed) + expired, errors.Join(rawErr, minuteErr)
}

// Convert only the captured legacy range. New projectors write the 24h deadline
// directly. No startup UPDATE/scan or large-table rebuild is involved.
func (s FlowDetailRetention) convertDeadlines(ctx context.Context, limit int) (int, bool, error) {
	n, migrating := 0, false
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var state model.FlowDetailRetentionState
		r := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&state, 1)
		if errors.Is(r.Error, gorm.ErrRecordNotFound) {
			var last model.FlowStartStatisticsPoint
			if err := tx.Select("source_id").Order("source_id DESC").Limit(1).Find(&last).Error; err != nil {
				return err
			}
			state = model.FlowDetailRetentionState{ID: 1, UpperID: last.SourceID}
			if err := tx.Create(&state).Error; err != nil {
				return err
			}
		} else if r.Error != nil {
			return r.Error
		}
		if state.AfterID >= state.UpperID {
			return nil
		}
		migrating = true
		var points []model.FlowStartStatisticsPoint
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("source_id,received_at,review_at").Where("source_id > ? AND source_id <= ?", state.AfterID, state.UpperID).Order("source_id").Limit(limit).Find(&points).Error; err != nil {
			return err
		}
		for _, p := range points {
			// Preserve explicit retry deferrals made after failed evidence checks.
			if p.ReviewAt.Equal(p.ReceivedAt.Add(metering.ObservationRetentionHorizon)) {
				if err := tx.Model(&model.FlowStartStatisticsPoint{}).Where("source_id = ?", p.SourceID).Update("review_at", p.ReceivedAt.Add(FlowStartDetailHorizon)).Error; err != nil {
					return err
				}
			}
			state.AfterID = p.SourceID
		}
		n = len(points)
		if n < limit {
			state.AfterID = state.UpperID
		}
		return tx.Save(&state).Error
	})
	return n, migrating, err
}

func (s FlowDetailRetention) expireMinutes(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	removed := int64(0)
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var state model.FlowStartStatisticsState
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&state, 1).Error; err != nil {
			return err
		}
		// Do not expire a legacy contribution before its captured source range is
		// reconciled. New/late sources are still admitted and projected normally.
		if state.LegacyAfterID != state.LegacyUpperID || state.LegacyProjectedRows != state.LegacyExpectedRows {
			return nil
		}
		var rows []model.FlowStartStatisticsMinute
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("minute_at < ?", cutoff).Order("minute_at,subscription_id,node_id,basis").Limit(limit).Find(&rows).Error; err != nil {
			return err
		}
		// Two ordered index seeks, rather than an OR/index-merge + temporary sort
		// across every historic and future bucket. The combined row budget stays fixed.
		if len(rows) < limit {
			var future []model.FlowStartStatisticsMinute
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("minute_at > ?", s.now().Add(FlowStartFutureAllowance).Truncate(time.Minute)).Order("minute_at,subscription_id,node_id,basis").Limit(limit - len(rows)).Find(&future).Error; err != nil {
				return err
			}
			rows = append(rows, future...)
		}
		keys := make([][]any, 0, len(rows))
		for _, r := range rows {
			keys = append(keys, []any{r.SubscriptionID, r.NodeID, r.Basis, r.MinuteAt})
		}
		if len(keys) == 0 {
			return nil
		}
		r := tx.Where("(subscription_id,node_id,basis,minute_at) IN ?", keys).Delete(&model.FlowStartStatisticsMinute{})
		removed = r.RowsAffected
		return r.Error
	})
	return int(removed), err
}
