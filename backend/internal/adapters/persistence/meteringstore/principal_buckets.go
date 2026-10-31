package meteringstore

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const PrincipalMinuteHorizon = 30 * 24 * time.Hour
const PrincipalDiagnosticHorizon = 24 * time.Hour

// Numeric metrics have deliberately lower resolution than billing. One bucket
// replaces arbitrarily many observations; exact financial/replay state is separate.
func recordPrincipalBucket(tx *gorm.DB, scope string, id uint, at time.Time, peak, count uint64, now time.Time) error {
	if at.IsZero() || id == 0 || (scope != ScopeUser && scope != ScopeSubscription) || peak > math.MaxInt64 || count > math.MaxInt64 || count == 0 {
		return errors.New("invalid principal statistics bucket")
	}
	if at.After(now) {
		at = now
	}
	if !at.Before(now.Add(-PrincipalMinuteHorizon)) {
		if err := upsertPrincipalMinute(tx, scope, id, at, peak, count); err != nil {
			return err
		}
	}
	greatest := "GREATEST(peak,VALUES(peak))"
	if tx.Dialector.Name() == "sqlite" {
		greatest = "MAX(peak,excluded.peak)"
	}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "scope_type"}, {Name: "scope_id"}, {Name: "hour_at"}}, DoUpdates: clause.Assignments(map[string]any{"peak": gorm.Expr(greatest), "sample_count": gorm.Expr("sample_count + ?", count)})}).Create(&model.PrincipalStatisticsHourly{ScopeType: scope, ScopeID: id, HourAt: at.UTC().Truncate(time.Hour), Peak: peak, SampleCount: count}).Error
}
func upsertPrincipalMinute(tx *gorm.DB, scope string, id uint, at time.Time, peak, count uint64) error {
	greatest := "GREATEST(peak,VALUES(peak))"
	if tx.Dialector.Name() == "sqlite" {
		greatest = "MAX(peak,excluded.peak)"
	}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "scope_type"}, {Name: "scope_id"}, {Name: "minute_at"}}, DoUpdates: clause.Assignments(map[string]any{"peak": gorm.Expr(greatest), "sample_count": gorm.Expr("sample_count + ?", count)})}).Create(&model.PrincipalStatisticsMinute{ScopeType: scope, ScopeID: id, MinuteAt: at.UTC().Truncate(time.Minute), Peak: peak, SampleCount: count}).Error
}

type PrincipalBucketMaintenance struct {
	DB  *gorm.DB
	Now func() time.Time
}

func (s PrincipalBucketMaintenance) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s PrincipalBucketMaintenance) HasPending(ctx context.Context) (bool, error) {
	var sample struct{ ObservationID uint64 }
	r := s.DB.WithContext(ctx).Model(&model.PrincipalStatisticsSample{}).Select("observation_id").Limit(1).Find(&sample)
	if r.Error != nil || r.RowsAffected > 0 {
		return r.RowsAffected > 0, r.Error
	}
	var minute model.PrincipalStatisticsMinute
	r = s.DB.WithContext(ctx).Select("minute_at").Where("minute_at < ?", s.now().Add(-PrincipalMinuteHorizon).Truncate(time.Hour)).Limit(1).Find(&minute)
	if r.Error != nil || r.RowsAffected > 0 {
		return r.RowsAffected > 0, r.Error
	}
	var state model.PrincipalStatisticsState
	if err := s.DB.WithContext(ctx).First(&state, 1).Error; err != nil {
		return false, err
	}
	if state.LegacyAfterID != state.LegacyUpperID || state.LegacyProjectedRows != state.LegacyExpectedRows {
		return false, nil
	}
	var raw PrincipalFlowScopeObservation
	r = s.DB.WithContext(ctx).Select("id,created_at").Order("id").Limit(1).Find(&raw)
	if r.Error != nil || r.RowsAffected == 0 {
		return false, r.Error
	}
	// Preserve the allocator recovery anchor used by cross-driver copies.
	if raw.ID >= state.ProjectedHead || !raw.CreatedAt.Before(s.now().Add(-PrincipalDiagnosticHorizon)) {
		return false, nil
	}
	var pending model.PrincipalStatisticsPending
	got := s.DB.WithContext(ctx).Select("observation_id").Where("observation_id = ?", raw.ID).Limit(1).Find(&pending)
	return got.RowsAffected == 0, got.Error
}

// Coarsen finite old event samples and retire diagnostics in small transactions.
// No ledger, replay receipt, completed-flow cursor or hourly total is removed.
// There is no startup history scan or whole-table DDL. A crash rolls back the
// minute contribution and sample deletion together, so retries cannot double count.
func (s PrincipalBucketMaintenance) Drain(ctx context.Context, limit int) (int, error) {
	if ctx == nil || s.DB == nil || limit < 1 || limit > 200 {
		return 0, errors.New("principal bucket maintenance requires 1..200 rows")
	}
	now := s.now()
	processed := 0
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var state model.PrincipalStatisticsState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&state, 1).Error; err != nil {
			return err
		}
		var samples []model.PrincipalStatisticsSample
		if err := tx.Select("observation_id,substr(scope_type,1,25) AS scope_type,scope_id,observed_at,active_flows").Order("observation_id").Limit(limit).Find(&samples).Error; err != nil {
			return err
		}
		type key struct {
			scope  string
			id     uint
			minute time.Time
		}
		values := map[key]model.PrincipalStatisticsMinute{}
		ids := make([]uint64, 0, len(samples))
		for _, p := range samples {
			if p.ScopeID == 0 || (p.ScopeType != ScopeUser && p.ScopeType != ScopeSubscription) || p.ObservedAt.IsZero() || p.ActiveFlows > math.MaxInt64 {
				return errors.New("invalid legacy principal metric; conversion held")
			}
			ids = append(ids, p.ObservationID)
			if p.ObservedAt.Before(now.Add(-PrincipalMinuteHorizon)) || p.ObservedAt.After(now) {
				continue
			}
			k := key{p.ScopeType, p.ScopeID, p.ObservedAt.UTC().Truncate(time.Minute)}
			v := values[k]
			v.SampleCount++
			if p.ActiveFlows > v.Peak {
				v.Peak = p.ActiveFlows
			}
			values[k] = v
		}
		// Deterministic scope/time lock order, shared with ordinary source writers.
		keys := make([]key, 0, len(values))
		for k := range values {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, b := keys[i], keys[j]
			if a.scope != b.scope {
				return a.scope == ScopeUser
			}
			if a.id != b.id {
				return a.id < b.id
			}
			return a.minute.Before(b.minute)
		})
		for _, k := range keys {
			v := values[k]
			if err := upsertPrincipalMinute(tx, k.scope, k.id, k.minute, v.Peak, v.SampleCount); err != nil {
				return err
			}
		}
		if len(ids) > 0 {
			if err := tx.Where("observation_id IN ?", ids).Delete(&model.PrincipalStatisticsSample{}).Error; err != nil {
				return err
			}
			processed += len(ids)
		}
		if state.LegacyAfterID == state.LegacyUpperID && state.LegacyProjectedRows == state.LegacyExpectedRows {
			var raws []PrincipalFlowScopeObservation
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id,created_at").Where("id < ?", state.ProjectedHead).Order("id").Limit(limit).Find(&raws).Error; err != nil {
				return err
			}
			due := make([]uint64, 0, len(raws))
			for _, raw := range raws {
				if !raw.CreatedAt.Before(now.Add(-PrincipalDiagnosticHorizon)) {
					break
				}
				due = append(due, raw.ID)
			}
			if len(due) > 0 {
				var pending int64
				if err := tx.Model(&model.PrincipalStatisticsPending{}).Where("observation_id IN ?", due).Count(&pending).Error; err != nil {
					return err
				}
				if pending == 0 {
					r := tx.Where("id IN ?", due).Delete(&PrincipalFlowScopeObservation{})
					if r.Error != nil {
						return r.Error
					}
					processed += int(r.RowsAffected)
				}
			}
		}
		// Hour-align expiry so partial-calendar reads always have a complete hour.
		var expired []model.PrincipalStatisticsMinute
		if err := tx.Select("scope_type,scope_id,minute_at").Where("minute_at < ?", now.Add(-PrincipalMinuteHorizon).Truncate(time.Hour)).Order("minute_at,scope_type,scope_id").Limit(limit).Find(&expired).Error; err != nil {
			return err
		}
		if len(expired) > 0 {
			keys := make([][]any, 0, len(expired))
			for _, p := range expired {
				keys = append(keys, []any{p.ScopeType, p.ScopeID, p.MinuteAt})
			}
			q := tx.Where("(scope_type,scope_id,minute_at) IN ?", keys).Delete(&model.PrincipalStatisticsMinute{})
			if q.Error != nil {
				return q.Error
			}
			processed += int(q.RowsAffected)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return processed, nil
}
