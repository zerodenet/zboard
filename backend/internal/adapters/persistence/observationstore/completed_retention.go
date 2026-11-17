package observationstore

import (
	"context"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const completedReceiptAge = 24 * time.Hour
const completedReceiptBatch = 200

type completedCursor struct {
	ID         uint      `gorm:"primaryKey;autoIncrement:false"`
	AfterID    string    `gorm:"size:64;not null"`
	NextPassAt time.Time `gorm:"not null"`
}

func (completedCursor) TableName() string { return "node_observation_gc_states" }

func (s MaterialRetention) hasCompletedScan(ctx context.Context) (bool, error) {
	if s.receiptAge() == 0 && s.materialAge() == 0 {
		return false, nil
	}
	var c completedCursor
	e := s.DB.WithContext(ctx).First(&c, 1).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return true, nil
	}
	return !c.NextPassAt.After(s.retentionNow()) || s.reviewPolicyChanged(c.NextPassAt), e
}

// PK pagination bounds discovery even when most old records are pinned, young
// or damaged. The durable cursor is constant-size; a crash may repeat an
// idempotent deletion but cannot skip uncommitted accounting (none lives here).
func (s MaterialRetention) pruneCompleted(ctx context.Context) error {
	if s.receiptAge() == 0 && s.materialAge() == 0 {
		return nil
	}
	now := s.retentionNow()
	c := completedCursor{ID: 1, NextPassAt: epoch}
	if e := s.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&c).Error; e != nil {
		return e
	}
	if e := s.DB.WithContext(ctx).First(&c, 1).Error; e != nil {
		return e
	}
	if c.NextPassAt.After(now) && !s.reviewPolicyChanged(c.NextPassAt) {
		return nil
	}
	var rows []model.NodeObservationIntent
	if e := s.DB.WithContext(ctx).Select("id,node_id,asset_tag,status,token,lease_until,material_retired_at,created_at,updated_at,material_review_at,material_bytes,part_count").Where("id > ?", c.AfterID).Order("id").Limit(completedReceiptBatch).Find(&rows).Error; e != nil {
		return e
	}
	for _, candidate := range rows {
		if s.receiptAge() > 0 && candidate.MaterialRetiredAt != nil && candidate.MaterialRetiredAt.Before(now.Add(-s.receiptAge())) && (candidate.Status == "retired" || candidate.Status == "applied") {
			if e := s.pruneOneCompleted(ctx, candidate, now); e != nil {
				return e
			}
		}
		// Shorter lifetimes revisit future review dates in this bounded sweep;
		// the administrator save never rewrites the entire observation table.
		anchor := candidate.CreatedAt
		if candidate.Status == "applied" {
			anchor = candidate.UpdatedAt
		}
		if s.materialAge() > 0 && candidate.MaterialRetiredAt == nil && candidate.Status != "retired" && anchor.Before(now.Add(-s.materialAge())) && candidate.MaterialReviewAt.After(now) {
			if e := s.DB.WithContext(ctx).Model(&model.NodeObservationIntent{}).Where("id=? AND material_retired_at IS NULL AND material_review_at>? AND status IN ?", candidate.ID, now, []string{"pending", "held", "applied"}).UpdateColumn("material_review_at", now).Error; e != nil {
				return e
			}
		}
		c.AfterID = candidate.ID
	}
	if len(rows) < completedReceiptBatch {
		c.AfterID = ""
		c.NextPassAt = now.Add(time.Hour)
	}
	return s.DB.WithContext(ctx).Save(&c).Error
}
func (s MaterialRetention) pruneOneCompleted(ctx context.Context, candidate model.NodeObservationIntent, now time.Time) error {
	return transaction(s.DB.WithContext(ctx), func(tx *gorm.DB) error {
		// Same node -> header -> retirement order as the legacy worker. No
		// financial, source-receipt, cursor or spool tables are touched.
		var node struct{ ID uint }
		q := tx.Model(&model.Node{}).Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Select("id").Where("id = ?", candidate.NodeID).Take(&node)
		if q.Error != nil && !errors.Is(q.Error, gorm.ErrRecordNotFound) {
			return q.Error
		}
		if q.RowsAffected == 0 {
			var count int64
			if e := tx.Model(&model.Node{}).Where("id = ?", candidate.NodeID).Count(&count).Error; e != nil {
				return e
			}
			if count > 0 {
				return nil
			}
		}
		var h model.NodeObservationIntent
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&h, "id = ?", candidate.ID).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return nil
			}
			return e
		}
		if h.MaterialRetiredAt == nil || !h.MaterialRetiredAt.Before(now.Add(-s.receiptAge())) || (h.Status != "retired" && h.Status != "applied") || h.Token != "" || !h.LeaseUntil.Equal(epoch) {
			return nil
		}
		for _, column := range []string{"latest_reference", "stats_reference", "floor_reference"} {
			var pin struct{ NodeID uint }
			r := tx.Model(&model.NodeObservationState{}).Select("node_id").Where(column+" = ?", h.ID).Limit(1).Scan(&pin)
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected > 0 {
				return nil
			}
		}
		// This is expired auxiliary history, not metering authority. Once body
		// cleanup has completed, retaining its original source hash/summary
		// forever adds no protection to accounting. Read only terminal metadata.
		var retirement struct {
			State          string
			RemainingBytes uint64
			UpdatedAt      time.Time
		}
		result := tx.Model(&model.NodeObservationRetirement{}).Clauses(clause.Locking{Strength: "UPDATE"}).Select("state,remaining_bytes,updated_at").Where("intent_id=?", h.ID).Scan(&retirement)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 || retirement.State != "complete" || retirement.RemainingBytes != 0 || retirement.UpdatedAt.After(now.Add(-s.receiptAge())) {
			return nil
		}

		for _, table := range []string{"node_observation_parts", "node_observation_retirement_parts"} {
			var p struct{ Ordinal uint64 }
			r := tx.Table(table).Select("ordinal").Where("intent_id = ?", h.ID).Limit(1).Scan(&p)
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected > 0 {
				return nil
			}
		}
		if e := tx.Where("intent_id = ?", h.ID).Delete(&model.NodeObservationRetirement{}).Error; e != nil {
			return e
		}
		return tx.Where("id = ?", h.ID).Delete(&model.NodeObservationIntent{}).Error
	})
}
