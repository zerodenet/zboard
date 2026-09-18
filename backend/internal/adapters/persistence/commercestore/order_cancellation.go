package commercestore

import (
	"context"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type OrderCancellation struct{ DB *gorm.DB }

func (s OrderCancellation) Cancel(ctx context.Context, actor, id uint, admin, force bool) (commerce.Order, error) {
	var order model.Order
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if actor == 0 || (force && !admin) {
			return commerce.ErrOrderPermission
		}
		if id == 0 {
			return commerce.ErrNotFound
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, id).Error; err != nil {
			return resourceError(err)
		}
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id", "email", "status", "is_admin").First(&user, actor).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commerce.ErrOrderPermission
			}
			return err
		}
		if user.Status != "active" || (admin && !user.IsAdmin) || (!admin && order.UserID != actor) {
			return commerce.ErrOrderPermission
		}
		if !commerce.OrderTransitionAllowed(order.Status, "canceled", force) {
			return commerce.ErrOrderNotCancelable
		}
		if order.Status == "canceled" {
			return nil
		}
		previous := order.Status
		now := time.Now().UTC()
		order.Status = "canceled"
		order.UpdatedAt = now
		order.CanceledAt = &now
		if err := tx.Model(&order).Updates(map[string]interface{}{"status": order.Status, "canceled_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "order.cancel", Target: fmt.Sprintf("order:%d", id), Detail: previous + "->canceled"}).Error
	})
	if err != nil {
		return commerce.Order{}, err
	}
	return commerce.Order(order), nil
}
