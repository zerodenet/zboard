package meteringstore

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PrincipalStatistics struct{ DB *gorm.DB }

func (s PrincipalStatistics) HasPending(ctx context.Context) (bool, error) {
	var state model.PrincipalStatisticsState
	if err := s.DB.WithContext(ctx).First(&state, "id = ?", 1).Error; err != nil {
		return false, err
	}
	if state.LegacyAfterID < state.LegacyUpperID {
		return true, nil
	}
	var head struct{ ObservationID uint64 }
	r := s.DB.WithContext(ctx).Model(&model.PrincipalStatisticsPending{}).Select("observation_id").Limit(1).Find(&head)
	return r.RowsAffected > 0, r.Error
}
func (s PrincipalStatistics) Drain(ctx context.Context, limit int) (int, error) {
	if s.DB == nil || ctx == nil || limit < 1 || limit > PrincipalStatisticsBatchLimit {
		return 0, errors.New("principal statistics batch must be1..200")
	}
	processed := 0
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// One bounded shared checkpoint serializes this auxiliary projector only.
		// The source writer never locks state; financial settlement never waits here.
		var state model.PrincipalStatisticsState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&state, "id = ?", 1).Error; err != nil {
			return err
		}
		if state.LegacyAfterID > state.LegacyUpperID || state.LegacyProjectedRows > state.LegacyExpectedRows {
			return errors.New("principal legacy projection coverage is corrupt")
		}
		legacy := state.LegacyAfterID < state.LegacyUpperID
		var ids []uint64
		if legacy {
			if err := tx.Model(&PrincipalFlowScopeObservation{}).Where("id > ? AND id <= ?", state.LegacyAfterID, state.LegacyUpperID).Order("id").Limit(limit).Pluck("id", &ids).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Model(&model.PrincipalStatisticsPending{}).Order("observation_id").Limit(limit).Pluck("observation_id", &ids).Error; err != nil {
				return err
			}
		}
		budget := principalStatisticsMaterialBudget
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return err
			}
			query := tx.Model(&PrincipalFlowScopeObservation{}).Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ?", id)
			// Metadata permits yielding before a valid next source would exceed this
			// batch's budget; oversize/corrupt individual evidence instead remains held.
			_, material := principalScopeLengths(tx)
			var head struct{ MaterialBytes int64 }
			if err := query.Session(&gorm.Session{}).Select(material + " AS material_bytes").Take(&head).Error; err != nil {
				return err
			}
			if head.MaterialBytes > 0 && head.MaterialBytes <= principalStatisticsRowBudget && head.MaterialBytes > budget {
				break
			}
			row, err := readPrincipalScopeSource(query, &budget)
			if err != nil {
				return err
			}
			// Match the writer/retainer lock order: raw source -> pending -> sample
			// -> hourly row. ID discovery above is deliberately nonlocking.
			var pending model.PrincipalStatisticsPending
			got := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("observation_id = ?", id).Limit(1).Find(&pending)
			if got.Error != nil {
				return got.Error
			}
			if got.RowsAffected > 0 {
				if pending.ScopeType != row.ScopeType || pending.ScopeID != row.ScopeID || !pending.ObservedAt.Equal(row.ObservedAt) {
					return errors.New("principal pending source identity differs")
				}
			} else if !legacy {
				return errors.New("principal pending checkpoint vanished")
			}
			if err := projectPrincipalSample(tx, row); err != nil {
				return err
			}
			if got.RowsAffected > 0 {
				if err := tx.Delete(&pending).Error; err != nil {
					return err
				}
			}
			if legacy {
				state.LegacyAfterID = id
				state.LegacyProjectedRows++
			}
			if id > state.ProjectedHead {
				state.ProjectedHead = id
			}
			processed++
		}
		if legacy && processed == len(ids) && len(ids) < limit {
			if state.LegacyProjectedRows != state.LegacyExpectedRows {
				return errors.New("principal legacy source count no longer matches captured coverage; restore before retiring material")
			}
			state.LegacyAfterID = state.LegacyUpperID
		}
		if legacy && state.LegacyAfterID == state.LegacyUpperID && state.LegacyProjectedRows != state.LegacyExpectedRows {
			return errors.New("principal legacy source coverage does not reconcile")
		}
		if processed == 0 && !legacy {
			return nil
		}
		if state.Revision >= math.MaxInt64 {
			return errors.New("principal projection checkpoint revision exhausted")
		}
		state.Revision++
		state.UpdatedAt = time.Now().UTC()
		return tx.Model(&model.PrincipalStatisticsState{}).Where("id = ?", 1).Updates(map[string]any{"legacy_after_id": state.LegacyAfterID, "legacy_projected_rows": state.LegacyProjectedRows, "projected_head": state.ProjectedHead, "revision": state.Revision, "updated_at": state.UpdatedAt}).Error
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	return processed, nil
}
func projectPrincipalSample(tx *gorm.DB, row PrincipalFlowScopeObservation) error {
	if row.ID == 0 || row.ScopeID == 0 || row.ObservedAt.IsZero() || row.CreatedAt.IsZero() || (row.ScopeType != ScopeUser && row.ScopeType != ScopeSubscription) || row.ActiveFlows > math.MaxInt64 {
		return errors.New("principal source scope/time/count cannot be projected")
	}
	digest := principalScopeDigest(row)
	var old model.PrincipalStatisticsSample
	r := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(principalStatisticsSampleColumns).Where("observation_id = ?", row.ID).Limit(1).Find(&old)
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected > 0 {
		matched, err := principalSampleSourceMatches(tx, old, row, digest)
		if err != nil {
			return err
		}
		if !matched {
			return errors.New("principal projected source changed; retain original evidence")
		}
		return nil
	}
	sample := model.PrincipalStatisticsSample{ObservationID: row.ID, ScopeType: row.ScopeType, ScopeID: row.ScopeID, EventID: row.EventID, ObservedAt: row.ObservedAt, ActiveFlows: row.ActiveFlows, SourceDigest: digest, SourceCreatedAt: row.CreatedAt, ReviewAt: row.CreatedAt.Add(metering.ObservationRetentionHorizon)}
	if err := tx.Create(&sample).Error; err != nil {
		return err
	}
	at := row.ObservedAt.UTC().Truncate(time.Hour)
	var hour model.PrincipalStatisticsHourly
	r = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("scope_type = ? AND scope_id = ? AND hour_at = ?", row.ScopeType, row.ScopeID, at).Limit(1).Find(&hour)
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected == 0 {
		return tx.Create(&model.PrincipalStatisticsHourly{ScopeType: row.ScopeType, ScopeID: row.ScopeID, HourAt: at, Peak: row.ActiveFlows, SampleCount: 1}).Error
	}
	if hour.SampleCount >= math.MaxInt64 || hour.Peak > math.MaxInt64 {
		return errors.New("principal hourly count exceeds exact supported range")
	}
	if row.ActiveFlows > hour.Peak {
		hour.Peak = row.ActiveFlows
	}
	hour.SampleCount++
	return tx.Model(&model.PrincipalStatisticsHourly{}).Where("scope_type = ? AND scope_id = ? AND hour_at = ?", hour.ScopeType, hour.ScopeID, hour.HourAt).Updates(map[string]any{"peak": hour.Peak, "sample_count": hour.SampleCount}).Error
}
func principalSampleMatches(sample model.PrincipalStatisticsSample, row PrincipalFlowScopeObservation, digest string) bool {
	return sample.ObservationID == row.ID && sample.ScopeType == row.ScopeType && sample.ScopeID == row.ScopeID && sample.EventID == row.EventID && sample.ActiveFlows == row.ActiveFlows && sample.SourceDigest == digest && len(sample.SourceDigest) == 64 && sample.ObservedAt.Equal(row.ObservedAt) && sample.SourceCreatedAt.Equal(row.CreatedAt)
}

const principalStatisticsSampleColumns = "observation_id,substr(scope_type,1,25) AS scope_type,scope_id,observed_at,active_flows,substr(source_digest,1,65) AS source_digest,source_created_at,review_at,raw_retired_at"

func principalStoredEventMatches(tx *gorm.DB, id uint64, event string) (bool, error) {
	compare := "BINARY event_id = BINARY ?"
	if tx.Dialector.Name() == "sqlite" {
		compare = "CAST(event_id AS BLOB) = CAST(? AS BLOB)"
	}
	var head struct{ ObservationID uint64 }
	q := tx.Model(&model.PrincipalStatisticsSample{}).Clauses(clause.Locking{Strength: "SHARE"}).Select("observation_id").Where("observation_id = ? AND "+compare, id, event).Limit(1).Find(&head)
	return q.RowsAffected == 1, q.Error
}
func principalSampleSourceMatches(tx *gorm.DB, sample model.PrincipalStatisticsSample, row PrincipalFlowScopeObservation, digest string) (bool, error) {
	// Compare the potentially variable original event key in the database rather
	// than allocating it from a corrupt/large sample. All other receipt fields
	// remain checked; an ordinary case-insensitive alias is not exact equality.
	sample.EventID = row.EventID
	if !principalSampleMatches(sample, row, digest) {
		return false, nil
	}
	return principalStoredEventMatches(tx, sample.ObservationID, row.EventID)
}
