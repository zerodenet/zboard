package entitlementstore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func CheckCapacity(tx *gorm.DB, planID uint, limit int, now time.Time) error {
	if limit <= 0 {
		return nil
	}
	// A locking current read avoids an earlier SKU identity snapshot under InnoDB
	// REPEATABLE READ. Read at most the threshold rather than all subscriptions.
	var active []model.Subscription
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("plan_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total", planID, "active", now).Limit(limit).Find(&active).Error; err != nil {
		return err
	}
	if len(active) >= limit {
		return entitlements.ErrCapacity
	}
	return nil
}
