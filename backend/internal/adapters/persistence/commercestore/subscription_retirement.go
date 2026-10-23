package commercestore

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// CancelRetiredSubscriptionOrders joins the lifecycle owner's transaction and
// uses the sole wallet release path. Keep target identity for late verified
// payments; clearing a target must never turn renewal into a new purchase.
func CancelRetiredSubscriptionOrders(tx *gorm.DB, sub model.Subscription, now time.Time) error {
	if err := stopAutomaticRenewal(tx, sub.ID, "window_closed", "订阅恢复窗口已结束，自动扣款已停止。", now); err != nil {
		return err
	}
	var orders []model.Order
	var ids []uint
	held := tx.Model(&model.OrderFunding{}).Select("1").Where("order_fundings.order_id = orders.id AND order_fundings.state = ?", "reserved")
	if err := tx.Model(&model.Order{}).Where("user_id = ? AND (target_subscription_id = ? OR subscription_id = ?)", sub.UserID, sub.ID, sub.ID).Where("(status = 'pending' OR (status IN ('canceled','failed') AND EXISTS (?)))", held).Order("id ASC").Limit(1001).Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) > 1000 {
		return commerce.ErrFundingConflict
	}
	// Lifecycle scope already owns these current orders and the user. Point
	// reads avoid acquiring another secondary-index range in reverse order.
	if len(ids) > 0 {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id ASC").Find(&orders).Error; err != nil {
			return err
		}
	}
	for _, order := range orders {
		if err := releaseOrderFunding(tx, order, "subscription_ended", now); err != nil {
			return err
		}
		if order.Status != "pending" {
			continue
		}
		if err := tx.Model(&order).Updates(map[string]any{"status": "canceled", "canceled_at": now, "updated_at": now, "failure_reason": "目标订阅已结束，不可恢复，请新购套餐。"}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &sub.UserID, Actor: "system", Action: "order.retire_cancel", Target: fmt.Sprintf("order:%d", order.ID), Detail: fmt.Sprintf("subscription:%d; reserved wallet released", sub.ID)}).Error; err != nil {
			return err
		}
	}
	return nil
}
