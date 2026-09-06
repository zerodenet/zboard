package handler

import (
	"context"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type orderResultCommand struct {
	OrderID  uint
	Status   string
	Force    bool
	Actor    authClaims
	Callback *string
}

type orderResult struct {
	Order     model.Order
	Fulfilled bool
}

// applyOrderResult owns the transaction shared by manual confirmation and
// recorded payment results. Callers authorize the actor and translate errors;
// this boundary does not read or synthesize HTTP responses.
func (h *handlers) applyOrderResult(ctx context.Context, command orderResultCommand) (orderResult, error) {
	var result orderResult
	err := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		order := &result.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(order, command.OrderID).Error; err != nil {
			return err
		}
		if !orderTransitionAllowed(order.Status, command.Status, command.Force) {
			return errOrderTransitionRejected
		}
		previous := order.Status
		now := time.Now().UTC()
		updates := make(map[string]interface{})
		if command.Status == orderStatusPaid {
			if previous != orderStatusPaid {
				if err := lockOrderSettlementUsers(tx, order.UserID, command.Actor.UserID); err != nil {
					return err
				}
			}
			if err := h.setOrderPaid(tx, order, now); err != nil {
				return err
			}
			result.Fulfilled = previous != orderStatusPaid
			if result.Fulfilled {
				if err := enqueueSubscriptionConfigPublishes(tx, order.SubscriptionID, command.Actor.UserID); err != nil {
					return err
				}
			}
		} else if previous != command.Status {
			order.Status = command.Status
			order.UpdatedAt = now
			updates["status"] = order.Status
			updates["updated_at"] = now
			if command.Status == orderStatusCanceled {
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
			return createAuditLog(tx, command.Actor, action, fmt.Sprintf("order:%d", order.ID), previous+"->"+order.Status)
		}
		return nil
	})
	if err != nil {
		return orderResult{}, err
	}
	return result, nil
}

// InnoDB checks audit_logs.user_id with a shared user-row lock. Acquire that
// lock alongside the buyer's exclusive lock, in ID order, before locking the
// plan. Otherwise an administrator buying the same plan can hold the actor row
// while waiting for a settlement that holds the plan and needs the audit FK.
// Shared actor locks allow unrelated buyers confirmed by one admin to proceed.
func lockOrderSettlementUsers(tx *gorm.DB, buyerID, actorID uint) error {
	ids := []uint{buyerID}
	if actorID != 0 && actorID != buyerID {
		if actorID < buyerID {
			ids = []uint{actorID, buyerID}
		} else {
			ids = append(ids, actorID)
		}
	}
	for _, id := range ids {
		strength := "SHARE"
		if id == buyerID {
			strength = "UPDATE"
		}
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: strength}).Select("id").First(&user, id).Error; err != nil {
			return err
		}
	}
	return nil
}
