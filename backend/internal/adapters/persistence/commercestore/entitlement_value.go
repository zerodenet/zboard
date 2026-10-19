package commercestore

import (
	"encoding/json"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"strconv"
	"time"
)

func entitlementValueState(sub model.Subscription) commerce.EntitlementValueState {
	return commerce.EntitlementValueState{PlanID: sub.PlanID, PlanSKUID: sub.PlanSKUID, StartAt: sub.StartAt, EndAt: sub.EndAt, Status: sub.Status, EndedAt: sub.EndedAt, EndReason: sub.EndReason, FlowTotal: sub.FlowTotal, FlowUsed: sub.FlowUsed, CycleStartUsed: sub.CycleStartUsed, ResetQuotaBytes: sub.ResetQuotaBytes, ResetPolicy: sub.ResetPolicy, NextResetAt: sub.NextResetAt}
}
func captureEntitlementValue(tx *gorm.DB, order *model.Order, sub model.Subscription, previous *model.Subscription, verified bool, now time.Time) error {
	// Read the authoritative persisted timestamps too: MySQL stores datetime(3),
	// while a Go grant may still carry nanoseconds before its write is rounded.
	var persisted model.Subscription
	if err := tx.First(&persisted, sub.ID).Error; err != nil {
		return err
	}
	sub = persisted
	if order.EntitlementValueSnapshot != "" {
		return fmt.Errorf("unpaid order already contains entitlement value evidence")
	}
	snap := commerce.EntitlementValueSnapshot{Version: 1, OrderID: order.ID, SubscriptionID: sub.ID, Currency: order.Currency, ActualPaidMinor: order.PayableAmount, PaymentVerified: verified, PaymentBasis: "manual_record_unverified", Kind: order.OrderType, Mode: "unsupported_legacy_effect", CapturedAt: now, TermStartAt: now, TermEndAt: sub.EndAt, BillingUnit: order.BillingUnit, BillingValue: order.BillingValue, AllocationModel: "uniform_paid_units_v1", After: entitlementValueState(sub)}
	if verified {
		snap.PaymentBasis = "verified_cash_or_wallet"
	}
	if order.PayableAmount == 0 {
		snap.PaymentVerified = true
		snap.PaymentBasis = "zero_additional_payment"
	}
	if previous != nil {
		before := entitlementValueState(*previous)
		snap.Before = &before
	}
	switch order.OrderType {
	case "new":
		snap.Mode = "new_term"
		snap.TermStartAt = sub.StartAt
	case "renewal":
		if previous != nil {
			if previous.EndedAt != nil || !previous.EndAt.After(now) {
				snap.Mode = "recovery_term"
				snap.TermStartAt = sub.StartAt
			} else if sub.EndAt.After(previous.EndAt) {
				snap.Mode = "extended_term"
				snap.TermStartAt = previous.EndAt
			}
		}
	case "traffic_reset":
		snap.Mode = "paid_reset"
		snap.AllocationModel = "reset_value_continuity_v1"
		if sub.NextResetAt != nil && sub.NextResetAt.Before(snap.TermEndAt) {
			snap.TermEndAt = *sub.NextResetAt
		}
	case "upgrade":
		snap.Mode = "legacy_change"
		snap.LegacyTransferUnproven = order.ChangeSnapshot != ""
		snap.AllocationModel = "legacy_value_requires_review"
	default:
		snap.AllocationModel = "legacy_value_requires_review"
	}
	var event model.QuotaEvent
	if err := tx.Where("subscription_id = ? AND reference_type = ? AND reference_id = ?", sub.ID, "order", strconv.FormatUint(uint64(order.ID), 10)).Order("id DESC").First(&event).Error; err != nil {
		return fmt.Errorf("capture actual entitlement event: %w", err)
	}
	snap.QuotaEventID = event.ID
	snap.QuotaEventAt = event.CreatedAt
	var purchase commerce.PurchaseSnapshot
	if json.Unmarshal([]byte(order.PurchaseSnapshot), &purchase) != nil || purchase.ValueAllocationModel == "" {
		snap.AllocationModel = "purchase_value_policy_unavailable"
	}
	if snap.AllocationModel == "uniform_paid_units_v1" && snap.PaymentVerified && (snap.Mode == "new_term" || snap.Mode == "extended_term" || snap.Mode == "recovery_term") {
		periods, allocationErr := commerce.UniformPaidPeriods(snap.TermStartAt, snap.TermEndAt, snap.BillingUnit, snap.BillingValue, snap.ActualPaidMinor)
		if allocationErr == nil {
			snap.PaidPeriods = periods
		} else {
			snap.AllocationModel = "value_requires_review"
		}
	}
	payload, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	result := tx.Model(&model.Order{}).Where("id = ? AND status = ? AND entitlement_value_snapshot = ?", order.ID, "pending", "").UpdateColumn("entitlement_value_snapshot", string(payload))
	// Force-paid legacy failed/cancelled orders remain supported, without
	// overwriting evidence if another transition already committed.
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		result = tx.Model(&model.Order{}).Where("id = ? AND status <> ? AND entitlement_value_snapshot = ?", order.ID, "paid", "").UpdateColumn("entitlement_value_snapshot", string(payload))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("entitlement value evidence changed before fulfillment commit")
		}
	}
	order.EntitlementValueSnapshot = string(payload)
	return nil
}
