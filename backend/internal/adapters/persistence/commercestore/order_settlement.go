package commercestore

import (
	"context"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type OrderSettlement struct {
	DB     *gorm.DB
	Issuer entitlements.CredentialIssuer
}
type settlementRecord struct {
	Order     model.Order
	Fulfilled bool
}

func (s OrderSettlement) Apply(ctx context.Context, actor uint, command commerce.SettlementCommand) (commerce.SettlementResult, error) {
	var result settlementRecord
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		order := &result.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(order, command.OrderID).Error; err != nil {
			return resourceError(err)
		}
		administrator, err := lockSettlementUsers(tx, order.UserID, actor)
		if err != nil {
			return err
		}
		if !commerce.OrderTransitionAllowed(order.Status, command.Status, command.Force) {
			return commerce.ErrOrderTransition
		}
		previous := order.Status
		now := time.Now().UTC()
		updates := make(map[string]interface{})
		if command.Status == "paid" {
			if err := s.setPaid(tx, order, now); err != nil {
				return err
			}
			result.Fulfilled = previous != "paid"
			if result.Fulfilled {
				if err := networkstore.EnqueueSubscriptionPublications(tx, order.SubscriptionID, actor); err != nil {
					return err
				}
			}
		} else if previous != command.Status {
			order.Status = command.Status
			order.UpdatedAt = now
			updates["status"] = order.Status
			updates["updated_at"] = now
			if command.Status == "canceled" {
				order.CanceledAt = &now
				updates["canceled_at"] = now
			}
		}
		action := "order.pay"
		if command.Callback != nil {
			action = "order.payment_result"
			order.RawCallback = *command.Callback
			order.UpdatedAt = now
			updates["raw_callback"] = order.RawCallback
			updates["updated_at"] = now
		}
		if len(updates) > 0 {
			if err := tx.Model(order).Updates(updates).Error; err != nil {
				return err
			}
		}
		if previous != order.Status {
			return tx.Create(&model.AuditLog{UserID: &administrator.ID, Actor: administrator.Email, Action: action, Target: fmt.Sprintf("order:%d", order.ID), Detail: previous + "->" + order.Status}).Error
		}
		return nil
	})
	if err != nil {
		return commerce.SettlementResult{}, err
	}
	return commerce.SettlementResult{Order: commerce.Order(result.Order), Fulfilled: result.Fulfilled}, nil
}
