package commercestore

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func replacementSettlementKey(order uint) string {
	return walletKey("full-change-order:" + strconv.FormatUint(uint64(order), 10))
}
func replacementReservationKey(order uint) string {
	return walletKey("reserve-entitlement-order:" + strconv.FormatUint(uint64(order), 10))
}

// Gross old residual enters the wallet, then the new purchase consumes its
// agreed funding. Both facts commit with the full replacement. WalletMinor is
// the net surplus; EntryID identifies the gross refund, not an extra top-up.
func settleFullReplacement(tx *gorm.DB, order model.Order, snapshot changeSnapshot, now time.Time) error {
	var old model.SubscriptionValueSettlement
	err := tx.Where("request_key = ?", replacementSettlementKey(order.ID)).First(&old).Error
	if err == nil {
		return commerce.ErrSubscriptionSettlementConflict
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	fund, configured, err := findOrderFunding(tx, order)
	if err != nil {
		return err
	}
	if !configured || fund.State != "reserved" || fund.EntitlementMinor != snapshot.Applied || !fund.ExpiresAt.After(now) {
		return commerce.ErrFundingConflict
	}
	var sub model.Subscription
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", *order.TargetSubscriptionID, order.UserID).First(&sub).Error; err != nil {
		return resourceError(err)
	}
	if err := validateFullReplacementQuote(tx, order, snapshot, now); err != nil {
		return err
	}
	var orders []model.Order
	if err := relatedSubscriptionOrders(tx, order.UserID, sub.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Find(&orders).Error; err != nil {
		return err
	}
	if len(orders) > 1000 {
		return commerce.ErrSubscriptionValueUnavailable
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	row := model.SubscriptionValueSettlement{UserID: order.UserID, SubscriptionID: sub.ID, RootOrderID: snapshot.SourceOrderID, RequestKey: replacementSettlementKey(order.ID), RequestFingerprint: walletKey(order.ChangeSnapshot), Currency: order.Currency, Kind: "change", AmountMinor: snapshot.Residual, WalletMinor: snapshot.Surplus, AppliedMinor: snapshot.Applied, ActorID: order.UserID, Reason: fmt.Sprintf("换购订单%d：结算旧权益并购买完整新期间", order.ID), QuoteSnapshot: string(payload), CreatedAt: now}
	if err := tx.Create(&row).Error; err != nil {
		return err
	}
	total, err := refundValueSources(tx, order.UserID, order.Currency, row.ID, snapshot.Sources, orders, now)
	if err != nil {
		return err
	}
	if total != snapshot.Residual {
		return commerce.ErrSubscriptionValueUnavailable
	}
	if total > 0 {
		entry, err := appendWalletEntry(tx, walletEntrySource{UserID: order.UserID, Currency: order.Currency}, walletKey(fmt.Sprintf("change-refund:%d", row.ID)), "subscription_change_refund", "换购旧权益结算入余额", total, 0, now)
		if err != nil {
			return err
		}
		if err := tx.Model(&row).Update("entry_id", entry).Error; err != nil {
			return err
		}
	}
	if snapshot.Applied > 0 {
		if _, err := appendWalletChange(tx, order, replacementReservationKey(order.ID), "reserve", "换购已结算权益用于本单余额预留", 0, snapshot.Applied, now); err != nil {
			return err
		}
	}
	finalTotal, finalUsed := entitlements.CycleQuota(entitlements.Subscription(sub))
	for _, previous := range orders {
		if previous.ID == order.ID || previous.SubscriptionEndedAt != nil && previous.Status != "pending" && previous.Status != "failed" {
			continue
		}
		updates := map[string]any{"subscription_ended_at": now, "subscription_end_reason": "changed", "subscription_final_flow_total": finalTotal, "subscription_final_flow_used": finalUsed, "updated_at": now}
		if previous.Status == "pending" || previous.Status == "failed" {
			if err := releaseOrderFunding(tx, previous, "subscription_changed", now); err != nil {
				return err
			}
			updates["status"] = "canceled"
			updates["canceled_at"] = now
		}
		if err := tx.Model(&model.Order{}).Where("id = ?", previous.ID).Updates(updates).Error; err != nil {
			return err
		}
	}
	return tx.Create(&model.AuditLog{UserID: &order.UserID, Actor: fmt.Sprintf("user:%d", order.UserID), Action: "subscription.change.settle", Target: fmt.Sprintf("order:%d", order.ID), Detail: fmt.Sprintf("settlement=%d residual=%d applied=%d surplus=%d currency=%s", row.ID, total, snapshot.Applied, snapshot.Surplus, order.Currency)}).Error
}

func verifyReplacementReservation(tx *gorm.DB, order model.Order, fund model.OrderFunding) error {
	if fund.EntitlementMinor == 0 {
		return nil
	}
	var settled model.SubscriptionValueSettlement
	if err := tx.Where("request_key = ? AND user_id = ?", replacementSettlementKey(order.ID), order.UserID).First(&settled).Error; err != nil {
		return commerce.ErrFundingConflict
	}
	if settled.Kind != "change" || settled.AppliedMinor != fund.EntitlementMinor || settled.Currency != order.Currency {
		return commerce.ErrFundingConflict
	}
	var entry model.WalletEntry
	if err := tx.Where("entry_key = ? AND user_id = ? AND currency = ?", replacementReservationKey(order.ID), order.UserID, order.Currency).First(&entry).Error; err != nil {
		return commerce.ErrFundingConflict
	}
	if entry.Kind != "reserve" || entry.BalanceDelta != 0 || entry.ReservedDelta != fund.EntitlementMinor || entry.SourceOrderID == nil || *entry.SourceOrderID != order.ID {
		return commerce.ErrFundingConflict
	}
	return nil
}

func configureFullReplacementFundingIfMissing(tx *gorm.DB, order model.Order, now time.Time) error {
	full, err := fullReplacementSnapshot(order)
	if err != nil || full == nil {
		return err
	}
	_, exists, err := findOrderFunding(tx, order)
	if err != nil || exists {
		return err
	}
	return configureOrderFunding(tx, order, commerce.FundingRequest{UseBalance: false}, now)
}
