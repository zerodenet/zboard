package commercestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PaymentFacts struct {
	DB     *gorm.DB
	Issuer entitlements.CredentialIssuer
}

func (s PaymentFacts) Record(ctx context.Context, authority commerce.PaymentFactAuthority, fact commerce.PaymentFact) (out commerce.PaymentFactResult, err error) {
	payload, err := json.Marshal(fact)
	if err != nil {
		return out, err
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := orderReader(tx, authority.AccountID, false); err != nil {
			return err
		}
		var order model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", fact.OrderID, authority.AccountID).First(&order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commerce.ErrOrderPermission
			}
			return err
		}
		var existing model.PaymentEvent
		existingErr := tx.Where("provider = ? AND provider_event_id = ?", authority.Provider, fact.ProviderEventID).First(&existing).Error
		if existingErr == nil {
			if existing.OrderID != order.ID || existing.EventType != fact.Status || existing.AmountMinor != fact.AmountMinor || existing.Payload != string(payload) {
				return commerce.ErrPaymentFactConflict
			}
			out = commerce.PaymentFactResult{OrderID: order.ID, Status: order.Status}
			return nil
		}
		if !errors.Is(existingErr, gorm.ErrRecordNotFound) {
			return existingErr
		}
		if fact.AmountMinor != order.PayableAmount || !strings.EqualFold(fact.Currency, order.Currency) {
			return commerce.ErrPaymentFactConflict
		}
		channel := authority.Provider
		if (order.Channel != "" && order.Channel != "manual" && order.Channel != channel) || (order.ProviderTradeNo != nil && *order.ProviderTradeNo != fact.ProviderTradeNo) {
			return commerce.ErrPaymentFactConflict
		}
		if !commerce.OrderTransitionAllowed(order.Status, fact.Status, false) {
			return commerce.ErrOrderTransition
		}
		now := time.Now().UTC()
		event := model.PaymentEvent{OrderID: order.ID, Provider: authority.Provider, ProviderEventID: fact.ProviderEventID, EventType: fact.Status, AmountMinor: fact.AmountMinor, SignatureValid: true, Payload: string(payload), CreatedAt: fact.OccurredAt.UTC()}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		previous := order.Status
		if fact.Status == "paid" {
			settlement := OrderSettlement{DB: s.DB, Issuer: s.Issuer}
			if err := settlement.setPaid(tx, &order, now); err != nil {
				return err
			}
			out.Fulfilled = previous != "paid"
			if out.Fulfilled {
				if err := networkstore.EnqueueSubscriptionPublications(tx, order.SubscriptionID, authority.AccountID); err != nil {
					return err
				}
			}
		} else if previous != "failed" {
			order.Status = "failed"
			order.FailureReason = "payment provider reported failure"
			order.UpdatedAt = now
			if err := tx.Model(&order).Updates(map[string]interface{}{"status": order.Status, "failure_reason": order.FailureReason, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&order).Updates(map[string]interface{}{"channel": channel, "provider_trade_no": fact.ProviderTradeNo}).Error; err != nil {
			return err
		}
		if err := tx.Model(&event).Update("processed_at", now).Error; err != nil {
			return err
		}
		if previous != order.Status {
			actor := authority.AccountID
			if err := tx.Create(&model.AuditLog{UserID: &actor, Actor: authority.Provider, Action: "commerce.payment.record", Target: fmt.Sprintf("order:%d", order.ID), Detail: previous + "->" + order.Status}).Error; err != nil {
				return err
			}
		}
		out.OrderID, out.Status = order.ID, order.Status
		return nil
	})
	return out, err
}
