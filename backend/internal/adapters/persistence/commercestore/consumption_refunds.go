package commercestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ConsumptionRefunds struct{ DB *gorm.DB }

func consumptionRefundSummary(row model.ConsumptionRefund) commerce.ConsumptionRefundSummary {
	return commerce.ConsumptionRefundSummary{ID: row.ID, OrderID: row.OrderID, UserID: row.UserID, Currency: row.Currency, AmountMinor: row.AmountMinor, TicketID: row.TicketID, ActorID: row.ActorID, Reason: row.Reason, EntryID: row.EntryID, CreatedAt: row.CreatedAt}
}

func (s ConsumptionRefunds) Approve(ctx context.Context, actor, orderID uint, in commerce.ConsumptionRefundRequest) (out commerce.ConsumptionRefundSummary, err error) {
	if !creationKeyPattern.MatchString(in.IdempotencyKey) {
		return out, &commerce.ValidationError{Message: "退款请求标识无效。"}
	}
	key := walletKey("consumption-refund:" + in.IdempotencyKey)
	payload, _ := json.Marshal(struct {
		OrderID uint
		Input   commerce.ConsumptionRefundRequest
	}{orderID, in})
	fingerprint := walletKey(string(payload))
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Keep the same source-order -> owner lock order as trusted settlement.
		// No consistent read precedes these locks: MySQL must see committed
		// source refunds after waiting for a competing financial transaction.
		var order model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, orderID).Error; err != nil {
			return resourceError(err)
		}
		administrator, err := lockSettlementUsers(tx, order.UserID, actor)
		if err != nil {
			return err
		}
		var prior model.ConsumptionRefund
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("request_key = ?", key).First(&prior).Error
		if err == nil {
			if prior.OrderID != orderID || prior.Fingerprint != fingerprint {
				return commerce.ErrConsumptionRefundConflict
			}
			out = consumptionRefundSummary(prior)
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if order.OrderType == "topup" {
			return &commerce.ValidationError{Message: "充值不是消费订单，不能通过消费退款重复增加余额。"}
		}
		// Active subscriptions use whole-subscription residual termination. This
		// historical-order approval never keeps an active term after a partial
		// refund or rewrites a newer term on the same subscription.
		if order.SubscriptionEndedAt == nil || order.SubscriptionEndReason == "" {
			return commerce.ErrConsumptionRefundReview
		}
		var ticket model.Ticket
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND user_id = ?", in.TicketID, order.UserID).First(&ticket).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &commerce.ValidationError{Message: "工单不存在或不属于原订单账户。"}
			}
			return err
		}
		now := time.Now().UTC()
		budget, err := lockOrderRefundBudget(tx, order, now)
		if err != nil {
			return err
		}
		if err := consumeOrderRefundBudget(tx, order, budget, in.AmountMinor, now); err != nil {
			return err
		}
		entryID, err := appendWalletCredit(tx, order, walletKey("consumption-refund-entry:"+key), "consumption_refund", fmt.Sprintf("获批消费退款：%s（工单#%d）", in.Reason, in.TicketID), in.AmountMinor, now)
		if err != nil {
			return err
		}
		row := model.ConsumptionRefund{RequestKey: key, Fingerprint: fingerprint, OrderID: orderID, UserID: order.UserID, Currency: order.Currency, AmountMinor: in.AmountMinor, TicketID: in.TicketID, ActorID: actor, Reason: in.Reason, EntryID: entryID, CreatedAt: now}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &actor, Actor: administrator.Email, Action: "order.consumption_refund", Target: fmt.Sprintf("order:%d", orderID), Detail: fmt.Sprintf("refund:%d user:%d amount:%d currency:%s ticket:%d reason:%s", row.ID, order.UserID, row.AmountMinor, order.Currency, row.TicketID, row.Reason)}).Error; err != nil {
			return err
		}
		out = consumptionRefundSummary(row)
		return nil
	})
	if uniqueViolation(err) || errors.Is(err, commerce.ErrPaymentFactConflict) {
		err = commerce.ErrConsumptionRefundConflict
	}
	if err != nil {
		out = commerce.ConsumptionRefundSummary{}
	}
	return
}

func (s ConsumptionRefunds) Read(ctx context.Context, actor, id uint) (out commerce.ConsumptionRefundSummary, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := orderReader(tx, actor, true); err != nil {
			return err
		}
		var row model.ConsumptionRefund
		if err := tx.First(&row, id).Error; err != nil {
			return resourceError(err)
		}
		out = consumptionRefundSummary(row)
		return nil
	})
	return
}
