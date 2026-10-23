package observationstore

import (
	"context"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const MaterialMinimumAge = 7 * 24 * time.Hour
const MaterialReviewDelay = time.Hour
const MaterialRetentionLimit = 32

type MaterialRetention struct {
	DB  *gorm.DB
	Now func() time.Time
}
type MaterialRetentionResult = network.ObservationMaterialRetentionResult

func (s MaterialRetention) Retire(ctx context.Context, limit int, budget int64) (out MaterialRetentionResult, err error) {
	if s.DB == nil || limit < 1 || limit > MaterialRetentionLimit || budget < MaxApplyBytes || budget > MaxDrainBytes {
		return out, errors.New("invalid observation material retention budget")
	}
	now := clockNow()
	if s.Now != nil {
		now = s.Now().UTC().Truncate(time.Millisecond)
	}
	for out.Inspected < limit {
		found, exhausted := false, false
		var retired bool
		var size uint64
		var materialErr error
		var candidateID string
		e := transaction(s.DB.WithContext(ctx), func(tx *gorm.DB) error {
			// Indexed metadata discovery is unlocked. A concurrent compactor may select
			// the same candidate; node/state/header revalidation decides ownership.
			var candidate model.NodeObservationIntent
			e := tx.Where("status = ? AND material_retired_at IS NULL AND material_review_at <= ?", "applied", now).Order("material_review_at,id").First(&candidate).Error
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return nil
			}
			if e != nil {
				return e
			}
			found = true
			candidateID = candidate.ID
			// Respect existing node -> exact asset state -> final header ordering.
			var node model.Node
			e = tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Select("id").First(&node, candidate.NodeID).Error
			if errors.Is(e, gorm.ErrRecordNotFound) {
				var n int64
				if e := tx.Model(&model.Node{}).Where("id = ?", candidate.NodeID).Count(&n).Error; e != nil {
					return e
				}
				if n != 0 {
					return deferMaterialReview(tx, candidate.ID, now, now.Add(time.Minute))
				}
			} else if e != nil {
				return e
			}
			var state model.NodeObservationState
			e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ? AND asset_tag = ?", candidate.NodeID, candidate.AssetTag).First(&state).Error
			if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			var h model.NodeObservationIntent
			e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", candidate.ID).First(&h).Error
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return nil
			}
			if e != nil {
				return e
			}
			// A busy concurrent visitor may defer scheduling after discovery.
			// Review time is scheduling only; node/state/header facts decide safety.
			if h.Status != "applied" || h.MaterialRetiredAt != nil {
				return nil
			}
			deferAt := now.Add(MaterialReviewDelay)
			if h.CreatedAt.IsZero() || h.UpdatedAt.IsZero() || h.UpdatedAt.Before(h.CreatedAt) {
				materialErr = invalid("retention candidate has inconsistent lifecycle times")
				return deferMaterialReview(tx, h.ID, now, deferAt)
			}
			if h.UpdatedAt.After(now.Add(-MaterialMinimumAge)) {
				if due := h.UpdatedAt.Add(MaterialMinimumAge); due.After(deferAt) {
					deferAt = due
				}
				return deferMaterialReview(tx, h.ID, now, deferAt)
			}
			// These point lookups require the three migration-owned reference indexes.
			// Preserve even invalid foreign-asset references as evidence for diagnosis.
			for _, column := range []string{"latest_reference", "stats_reference", "floor_reference"} {
				var pin struct{ NodeID uint }
				q := tx.Model(&model.NodeObservationState{}).Select("node_id").Where(column+" = ?", h.ID).Limit(1).Scan(&pin)
				if q.Error != nil {
					return q.Error
				}
				if q.RowsAffected != 0 {
					return deferMaterialReview(tx, h.ID, now, deferAt)
				}
			}
			if h.NodeID == 0 || h.AssetTag == "" || state.NodeID != h.NodeID || state.AssetTag != h.AssetTag || h.Token != "" || !h.LeaseUntil.Equal(epoch) || h.MaterialBytes == 0 || h.MaterialBytes > uint64(MaxApplyBytes) {
				materialErr = invalid("retention candidate has invalid applied ownership")
				return deferMaterialReview(tx, h.ID, now, deferAt)
			}
			if h.MaterialBytes > uint64(budget) {
				exhausted = true
				return nil
			}
			// Spend the bound before reading immutable blobs, including rejected input.
			budget -= int64(h.MaterialBytes)
			source, e := readSource(tx, h)
			if e != nil {
				if !errors.Is(e, ErrMaterial) {
					return e
				}
				materialErr = e
				return deferMaterialReview(tx, h.ID, now, deferAt)
			}
			if source.LegacyFloor || source.Observation.Latest.AssetTag != h.AssetTag || !usable(source.Observation.Latest) {
				materialErr = invalid("retention source identity invalid")
				return deferMaterialReview(tx, h.ID, now, deferAt)
			}
			deleted := tx.Where("intent_id = ?", h.ID).Delete(&model.NodeObservationPart{})
			if deleted.Error != nil {
				return deleted.Error
			}
			if uint64(deleted.RowsAffected) != h.PartCount {
				return invalid("retirement part count changed")
			}
			r := tx.Model(&model.NodeObservationIntent{}).Where("id = ? AND status = ? AND token = ? AND material_retired_at IS NULL", h.ID, "applied", "").UpdateColumns(map[string]any{"material_retired_at": now})
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return ErrOwnership
			}
			retired, size = true, h.MaterialBytes
			return nil
		})
		if e != nil {
			return out, errors.Join(err, e)
		}
		if !found || exhausted {
			return out, err
		}
		out.Inspected++
		if retired {
			out.Retired++
			out.Bytes += size
		}
		if materialErr != nil {
			err = errors.Join(err, fmt.Errorf("observation material %s: %w", candidateID, materialErr))
		}
		if ctx.Err() != nil {
			return out, errors.Join(err, ctx.Err())
		}
	}
	return out, err
}

// Scheduling is auxiliary metadata. Do not refresh original disposition time,
// alter freshness/reason, or claim the material has retired.
func deferMaterialReview(tx *gorm.DB, id string, now, next time.Time) error {
	return tx.Model(&model.NodeObservationIntent{}).Where("id = ? AND status = ? AND material_retired_at IS NULL AND material_review_at <= ?", id, "applied", now).UpdateColumns(map[string]any{"material_review_at": next}).Error
}

func (s MaterialRetention) HasPending(ctx context.Context) (bool, error) {
	if s.DB == nil {
		return false, errors.New("observation material retention unavailable")
	}
	now := clockNow()
	if s.Now != nil {
		now = s.Now().UTC().Truncate(time.Millisecond)
	}
	var h struct{ ID string }
	r := s.DB.WithContext(ctx).Model(&model.NodeObservationIntent{}).Select("id").Where("status = ? AND material_retired_at IS NULL AND material_review_at <= ?", "applied", now).Limit(1).Scan(&h)
	return r.RowsAffected > 0, r.Error
}
