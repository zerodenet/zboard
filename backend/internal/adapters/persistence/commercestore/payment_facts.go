package commercestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	Now    func() time.Time
	DB     *gorm.DB
	Issuer entitlements.CredentialIssuer
}

func (s PaymentFacts) Record(ctx context.Context, authority commerce.PaymentFactAuthority, fact commerce.PaymentFact) (out commerce.PaymentFactResult, err error) {
	payload, err := json.Marshal(fact)
	if err != nil {
		return out, err
	}
	err = withSubscriptionSettlementScope(s.DB.WithContext(ctx), fact.OrderID, authority.AccountID, authority.AccountID, authority.RequireAdministrator, func(tx *gorm.DB, identity *subscriptionSettlementIdentity) error {
		var order model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", fact.OrderID, authority.AccountID).First(&order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commerce.ErrOrderPermission
			}
			return err
		}
		if err := validateSubscriptionSettlementIdentity(order, identity); err != nil {
			return err
		}
		// Match cancellation/administrator settlement: order before account.
		if err := orderReader(tx, authority.AccountID, authority.RequireAdministrator); err != nil {
			return err
		}
		out = commerce.PaymentFactResult{OrderID: order.ID, Status: order.Status}
		var existing model.PaymentEvent
		existingErr := tx.Where("provider = ? AND provider_event_id = ?", authority.Provider, fact.ProviderEventID).First(&existing).Error
		if existingErr == nil {
			if existing.OrderID != order.ID || existing.EventType != fact.Status || existing.AmountMinor != fact.AmountMinor || !samePaymentFact(existing.Payload, fact) {
				return commerce.ErrPaymentFactConflict
			}
			if fact.Status == "paid" {
				var receipt model.PaymentReceipt
				err := tx.Where("receipt_key = ?", paymentReceiptKey(authority.Provider, fact.ProviderTradeNo)).First(&receipt).Error
				if err == nil {
					out.Disposition = receipt.Disposition
				} else if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
			}
			return nil
		}
		if !errors.Is(existingErr, gorm.ErrRecordNotFound) {
			return existingErr
		}
		fund, configured, err := findOrderFunding(tx, order)
		if err != nil {
			return err
		}
		full, err := fullReplacementSnapshot(order)
		if err != nil {
			return err
		}
		expected := order.PayableAmount
		if full != nil {
			expected -= full.Applied
		}
		if configured {
			expected = fund.CashMinor
		}
		// Pre-host integrations may have collected the original full quote
		// before a wallet split existed. Preserve verified whole-order cash,
		// without capturing the wallet or using it to fulfill a changed split.
		unplannedWhole := (configured || full != nil) && authority.RequireAdministrator && !authority.RequireAttempt && fact.Status == "paid" && fact.AmountMinor == order.PayableAmount && fact.AmountMinor != expected
		if (fact.AmountMinor != expected && !unplannedWhole) || !strings.EqualFold(fact.Currency, order.Currency) {
			return commerce.ErrPaymentFactConflict
		}
		if authority.RequireAttempt {
			var attempt model.PaymentAttempt
			if err := tx.Where("order_id = ? AND provider = ?", order.ID, authority.Provider).First(&attempt).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return commerce.ErrPaymentFactConflict
				}
				return err
			}
			if attempt.AmountMinor != fact.AmountMinor || !strings.EqualFold(attempt.Currency, fact.Currency) {
				return commerce.ErrPaymentFactConflict
			}
		}
		now := commerceNow(s.Now)
		event := model.PaymentEvent{OrderID: order.ID, Provider: authority.Provider, ProviderEventID: fact.ProviderEventID, EventType: fact.Status, AmountMinor: fact.AmountMinor, SignatureValid: true, Payload: string(payload), CreatedAt: fact.OccurredAt.UTC()}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		if fact.Status != "paid" {
			if (order.Channel != "" && order.Channel != "manual" && order.Channel != authority.Provider) || (order.ProviderTradeNo != nil && *order.ProviderTradeNo != fact.ProviderTradeNo) {
				return commerce.ErrPaymentFactConflict
			}
			if !commerce.OrderTransitionAllowed(order.Status, fact.Status, false) {
				return commerce.ErrOrderTransition
			}
			if err := tx.Model(&order).Updates(map[string]any{"status": "failed", "failure_reason": "payment provider reported failure", "channel": authority.Provider, "provider_trade_no": fact.ProviderTradeNo, "updated_at": now}).Error; err != nil {
				return err
			}
			out.Status = "failed"
			if err := releaseOrderFunding(tx, order, "payment_failed", now); err != nil {
				return err
			}
		} else {
			receipt, fresh, err := acceptPaymentReceipt(tx, order, authority, fact, now)
			if err != nil {
				return err
			}
			if fresh {
				if err := s.applyReceipt(tx, &order, &receipt, now, unplannedWhole); err != nil {
					return err
				}
				out.Fulfilled = receipt.Disposition == "fulfilled" || receipt.Disposition == "wallet_topup"
			}
			out.Status, out.Disposition = order.Status, receipt.Disposition
		}
		if err := tx.Model(&event).Update("processed_at", commerceNow(s.Now)).Error; err != nil {
			return err
		}
		actor := authority.AccountID
		return tx.Create(&model.AuditLog{UserID: &actor, Actor: authority.Provider, Action: "commerce.payment.record", Target: fmt.Sprintf("order:%d", order.ID), Detail: fact.Status + ":" + out.Disposition}).Error
	})
	if uniqueViolation(err) {
		return commerce.PaymentFactResult{}, commerce.ErrPaymentFactConflict
	}
	return out, err
}

func paymentReceiptKey(provider, trade string) string { return walletKey(provider + "\x00" + trade) }

func acceptPaymentReceipt(tx *gorm.DB, order model.Order, authority commerce.PaymentFactAuthority, fact commerce.PaymentFact, now time.Time) (receipt model.PaymentReceipt, fresh bool, err error) {
	key := paymentReceiptKey(authority.Provider, fact.ProviderTradeNo)
	err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("receipt_key = ?", key).First(&receipt).Error
	if err == nil {
		if receipt.OrderID != order.ID || receipt.Provider != authority.Provider || receipt.ProviderTradeNo != fact.ProviderTradeNo || receipt.AmountMinor != fact.AmountMinor || receipt.Currency != fact.Currency {
			return receipt, false, commerce.ErrPaymentFactConflict
		}
		return receipt, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return receipt, false, err
	}
	receipt = model.PaymentReceipt{ReceiptKey: key, Provider: authority.Provider, ProviderTradeNo: fact.ProviderTradeNo, OrderID: order.ID, AmountMinor: fact.AmountMinor, Currency: fact.Currency, Disposition: "processing", CreatedAt: now, ProcessedAt: now}
	// Old paid orders already consumed this bound receipt. Do not backfill a
	// second wallet credit, or invent the identity of manually paid legacy orders.
	if order.Status == "paid" && order.Channel == authority.Provider && order.ProviderTradeNo != nil && *order.ProviderTradeNo == fact.ProviderTradeNo {
		receipt.Disposition = "legacy_applied"
	}
	err = tx.Create(&receipt).Error
	return receipt, err == nil && receipt.Disposition != "legacy_applied", err
}

func (s PaymentFacts) applyReceipt(tx *gorm.DB, order *model.Order, receipt *model.PaymentReceipt, now time.Time, unplannedWhole bool) error {
	if order.PaidAmount > 0 && order.ProviderTradeNo == nil && order.Channel != "wallet" && order.OrderType != "topup" {
		// Legacy/manual payment has no verifiable channel receipt identity. Keep
		// collected money visible for reconciliation; never invent a second value.
		receipt.Disposition = "review_required"
		return tx.Model(receipt).Update("disposition", receipt.Disposition).Error
	}
	fulfill := order.Status == "pending" || order.Status == "failed" || (order.OrderType == "topup" && order.Status == "canceled")
	fund, configured, err := findOrderFunding(tx, *order)
	if err != nil {
		return err
	}
	if configured && (unplannedWhole || fund.State != "reserved" || !fund.ExpiresAt.After(now)) {
		reason := "expired"
		if unplannedWhole {
			reason = "funding_mismatch"
		}
		if err := releaseOrderFunding(tx, *order, reason, now); err != nil {
			return err
		}
		fulfill = false
		if order.Status == "pending" || order.Status == "failed" {
			if err := tx.Model(order).Updates(map[string]any{"status": "canceled", "canceled_at": now, "updated_at": now}).Error; err != nil {
				return err
			}
			order.Status = "canceled"
		}
	}
	if fulfill {
		// Validation can become stale after real collection. Roll back entitlement
		// changes only; retain the actual receipt and credit cash to its owner.
		if err := tx.SavePoint("payment_fulfillment").Error; err != nil {
			return err
		}
		settlement := OrderSettlement{DB: s.DB, Issuer: s.Issuer, Now: s.Now}
		err := settlement.setVerifiedPaid(tx, order, now)
		if err == nil {
			receipt.Disposition = "fulfilled"
			if order.OrderType == "topup" {
				receipt.Disposition = "wallet_topup"
				var entry model.WalletEntry
				if err := tx.Where("entry_key = ?", walletKey("topup-order:"+strconv.FormatUint(uint64(order.ID), 10))).First(&entry).Error; err != nil {
					return err
				}
				receipt.WalletEntryID = &entry.ID
			} else if order.SubscriptionID != 0 {
				if err := networkstore.EnqueueSubscriptionPublications(tx, order.SubscriptionID, order.UserID); err != nil {
					return err
				}
			}
			if err := tx.Model(order).Updates(map[string]any{"channel": receipt.Provider, "provider_trade_no": receipt.ProviderTradeNo}).Error; err != nil {
				return err
			}
		} else {
			now = commerceNow(s.Now)
			var invalid *commerce.ValidationError
			if !errors.As(err, &invalid) && !errors.Is(err, entitlements.ErrCapacity) {
				return err
			}
			if err := tx.RollbackTo("payment_fulfillment").Error; err != nil {
				return err
			}
			if err := tx.First(order, order.ID).Error; err != nil {
				return err
			}
			reason := "付款已核验，权益条件已变化，款项已转入站内余额，请重新确认订单。"
			if invalid != nil {
				switch invalid.Message {
				case entitlements.ErrGrantTargetRequired.Error():
					reason = "付款已核验，但历史订单缺少独立订阅目标；款项已转入余额，请通过工单核对。"
				case entitlements.ErrRenewalWindow.Error():
					reason = "付款已核验，但订阅已结束或已超过续费恢复窗口；款项已转入余额，请重新确认可售操作。"
				}
			}
			if err := tx.Model(order).Updates(map[string]any{"status": "canceled", "canceled_at": now, "failure_reason": reason, "updated_at": now}).Error; err != nil {
				return err
			}
			order.Status = "canceled"
			fulfill = false
		}
	}
	if !fulfill {
		if err := releaseOrderFunding(tx, *order, "fulfillment_unavailable", now); err != nil {
			return err
		}
		entryID, err := appendWalletCredit(tx, *order, walletKey("payment-receipt:"+strconv.FormatUint(uint64(receipt.ID), 10)), "payment_credit", "已核验的迟到或重复渠道付款转入余额", receipt.AmountMinor, now)
		if err != nil {
			return err
		}
		receipt.Disposition, receipt.WalletEntryID = "wallet_credit", &entryID
	}
	receipt.ProcessedAt = now
	return tx.Model(receipt).Updates(map[string]any{"disposition": receipt.Disposition, "wallet_entry_id": receipt.WalletEntryID, "processed_at": now}).Error
}

// MySQL's JSON column normalizes whitespace and object ordering. Replay
// identity is the immutable payment fact, never the database's JSON spelling.
func samePaymentFact(payload string, fact commerce.PaymentFact) bool {
	var stored commerce.PaymentFact
	if json.Unmarshal([]byte(payload), &stored) != nil {
		return false
	}
	return stored.OrderID == fact.OrderID && stored.ProviderEventID == fact.ProviderEventID &&
		stored.ProviderTradeNo == fact.ProviderTradeNo && stored.Status == fact.Status &&
		stored.AmountMinor == fact.AmountMinor && stored.Currency == fact.Currency &&
		stored.OccurredAt.Equal(fact.OccurredAt)
}
