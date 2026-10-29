package meteringstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PrincipalScopeRetention struct {
	DB  *gorm.DB
	Now func() time.Time
}

func (s PrincipalScopeRetention) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s PrincipalScopeRetention) HasPending(ctx context.Context) (bool, error) {
	var state model.PrincipalStatisticsState
	if err := s.DB.WithContext(ctx).First(&state, "id = ?", 1).Error; err != nil {
		return false, err
	}
	if state.LegacyAfterID != state.LegacyUpperID || state.LegacyProjectedRows != state.LegacyExpectedRows {
		return false, nil
	}
	var head struct{ ObservationID uint64 }
	r := s.DB.WithContext(ctx).Model(&model.PrincipalStatisticsSample{}).Select("observation_id").Where("raw_retired_at IS NULL AND observation_id < COALESCE((SELECT MAX(id) FROM principal_flow_scope_observations),0) AND source_created_at < ? AND review_at <= ?", s.now().Add(-metering.ObservationRetentionHorizon), s.now()).Order("review_at,observation_id").Limit(1).Find(&head)
	return r.RowsAffected > 0, r.Error
}

// Retire only proven scope diagnostics, never financial/connection authority.
// Exact-time samples, original event identities/digests and hourly values remain.
// This is deliberately separate from projection and from the financial writer.
func (s PrincipalScopeRetention) Retire(ctx context.Context, limit int, budget int64) (int, error) {
	if s.DB == nil || ctx == nil || limit < 1 || limit > 32 || budget < principalStatisticsRowBudget || budget > principalStatisticsMaterialBudget {
		return 0, errors.New("principal scope retention requires1..32 rows and bounded material budget")
	}
	now := s.now()
	cutoff := now.Add(-metering.ObservationRetentionHorizon)
	var ids []uint64
	if err := s.DB.WithContext(ctx).Model(&model.PrincipalStatisticsSample{}).Where("raw_retired_at IS NULL AND observation_id < COALESCE((SELECT MAX(id) FROM principal_flow_scope_observations),0) AND source_created_at < ? AND review_at <= ?", cutoff, now).Order("review_at,observation_id").Limit(limit).Pluck("observation_id", &ids).Error; err != nil {
		return 0, err
	}
	retired := 0
	var failures []error
	for _, id := range ids {
		exhausted, changed := false, false
		var held error
		err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var state model.PrincipalStatisticsState
			// Same state->source->sample lock order as projection. Source writers never
			// request state; their existing node/scope locks are not held by this task.
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&state, "id = ?", 1).Error; err != nil {
				return err
			}
			if state.LegacyAfterID != state.LegacyUpperID || state.LegacyProjectedRows != state.LegacyExpectedRows {
				return nil
			}
			query := tx.Model(&PrincipalFlowScopeObservation{}).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id)
			core, material := principalScopeLengths(tx)
			var head struct {
				MaterialBytes int64
				CoreBytes     int64
			}
			source := query.Session(&gorm.Session{}).Select(material + " AS material_bytes," + core + " AS core_bytes").Limit(1).Find(&head)
			if source.Error != nil {
				return source.Error
			}
			var sample model.PrincipalStatisticsSample
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(principalStatisticsSampleColumns).Where("observation_id = ?", id).Take(&sample).Error; err != nil {
				return err
			}
			if sample.RawRetiredAt != nil || !sample.SourceCreatedAt.Before(cutoff) || sample.ReviewAt.After(now) {
				return nil
			}
			var floor struct{ Maximum uint64 }
			if err := tx.Model(&PrincipalFlowScopeObservation{}).Select("COALESCE(MAX(id),0) AS maximum").Scan(&floor).Error; err != nil {
				return err
			}
			if id >= floor.Maximum {
				return tx.Model(&model.PrincipalStatisticsSample{}).Where("observation_id = ?", id).Update("review_at", now.Add(time.Hour)).Error
			}

			hold := func(reason error) error {
				held = reason
				return tx.Model(&model.PrincipalStatisticsSample{}).Where("observation_id = ?", id).Update("review_at", now.Add(time.Hour)).Error
			}
			if source.RowsAffected == 0 {
				return hold(errors.New("unretired principal diagnostic source is missing; restore before retirement"))
			}
			if head.CoreBytes < 0 || head.CoreBytes > metering.MaxRuntimeIdentityBytes || head.MaterialBytes < 0 || head.MaterialBytes > principalStatisticsRowBudget {
				return hold(errors.New("oversized principal diagnostic retained without reading body"))
			}
			if head.MaterialBytes > 0 && head.MaterialBytes <= principalStatisticsRowBudget && head.MaterialBytes > budget {
				exhausted = true
				return nil
			}
			row, err := readPrincipalScopeSource(query, &budget)
			if err != nil {
				return err
			}
			matched, err := principalSampleSourceMatches(tx, sample, row, principalScopeDigest(row))
			if err != nil {
				return err
			}
			if !matched {
				return hold(errors.New("principal source no longer matches complete statistical receipt; retained original"))
			}
			var pending int64
			if err := tx.Model(&model.PrincipalStatisticsPending{}).Where("observation_id = ?", id).Count(&pending).Error; err != nil {
				return err
			}
			if pending != 0 {
				return tx.Model(&model.PrincipalStatisticsSample{}).Where("observation_id = ?", id).Update("review_at", now.Add(time.Minute)).Error
			}
			// The full source digest was verified under the same primary-key lock.
			result := tx.Where("id = ?", id).Delete(&PrincipalFlowScopeObservation{})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("principal diagnostic retirement changed its source count")
			}
			if err := tx.Model(&model.PrincipalStatisticsSample{}).Where("observation_id = ? AND raw_retired_at IS NULL", id).Update("raw_retired_at", now).Error; err != nil {
				return err
			}
			changed = true
			return nil
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			return retired, errors.Join(append(failures, err)...)
		}
		if exhausted {
			break
		}
		if changed {
			retired++
		}
		if held != nil {
			failures = append(failures, held)
		}
	}
	return retired, errors.Join(failures...)
}
