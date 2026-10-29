package commercestore

import (
	"encoding/json"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
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
	case "traffic_pack":
		// Capture this grant's actual paid quota and unchanged term. Historical
		// unsupported records are never reconstructed from today's catalog.
		snap.AllocationModel = "legacy_value_requires_review"
		sameReset := previous != nil && ((previous.NextResetAt == nil && sub.NextResetAt == nil) ||
			(previous.NextResetAt != nil && sub.NextResetAt != nil && previous.NextResetAt.Equal(*sub.NextResetAt)))
		if previous != nil && snap.PaymentVerified && !entitlements.IsPerpetualEnd(sub.EndAt) &&
			sub.StartAt.Equal(previous.StartAt) && sub.EndAt.Equal(previous.EndAt) &&
			sub.PlanID == previous.PlanID && sub.PlanSKUID == previous.PlanSKUID &&
			sub.ResetPolicy == previous.ResetPolicy && sub.ResetQuotaBytes == previous.ResetQuotaBytes && sameReset &&
			sub.FlowUsed == previous.FlowUsed && sub.CycleStartUsed == previous.CycleStartUsed &&
			order.TrafficBytes > 0 && sub.FlowTotal > previous.FlowTotal && sub.FlowTotal-previous.FlowTotal == order.TrafficBytes {
			snap.Mode, snap.AllocationModel = "timed_quota_addition", commerce.TimedQuotaAdditionAllocation
			if sub.NextResetAt != nil && sub.NextResetAt.Before(snap.TermEndAt) {
				snap.TermEndAt = *sub.NextResetAt
			}
		}
	case "upgrade":
		full, err := fullReplacementSnapshot(*order)
		if err != nil {
			return err
		}
		if full != nil {
			snap.Mode = "replacement_term"
			snap.TermStartAt = sub.StartAt
		} else {
			snap.Mode = "legacy_change"
			snap.LegacyTransferUnproven = order.ChangeSnapshot != ""
			snap.AllocationModel = "legacy_value_requires_review"
		}
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
	if previous != nil && snap.AllocationModel != "purchase_value_policy_unavailable" && snap.PaymentVerified &&
		entitlements.IsPerpetualEnd(previous.EndAt) && sub.EndAt.Equal(previous.EndAt) && sub.StartAt.Equal(previous.StartAt) &&
		previous.FlowUsed == sub.FlowUsed && previous.CycleStartUsed == sub.CycleStartUsed && sub.FlowTotal > previous.FlowTotal &&
		sub.FlowTotal-previous.FlowTotal == order.TrafficBytes && sub.NextResetAt == nil && (sub.ResetPolicy == 0 || sub.ResetPolicy == 5) &&
		(order.OrderType == "traffic_pack" || order.OrderType == "renewal" && order.BillingUnit == "once") {
		snap.Mode, snap.AllocationModel = "perpetual_quota_addition", commerce.PerpetualQuotaFIFOAllocation
	}
	if snap.AllocationModel == "uniform_paid_units_v1" && snap.PaymentVerified && (snap.Mode == "new_term" || snap.Mode == "extended_term" || snap.Mode == "recovery_term" || snap.Mode == "replacement_term") {
		if snap.BillingUnit == "once" && snap.BillingValue == 1 && entitlements.IsPerpetualEnd(snap.TermEndAt) && (sub.ResetPolicy == 0 || sub.ResetPolicy == 5) && sub.NextResetAt == nil && snap.Mode != "extended_term" {
			snap.AllocationModel = commerce.PerpetualQuotaAllocation
		} else {
			periods, allocationErr := commerce.UniformPaidPeriods(snap.TermStartAt, snap.TermEndAt, snap.BillingUnit, snap.BillingValue, snap.ActualPaidMinor)
			if allocationErr == nil {
				snap.PaidPeriods = periods
			} else {
				snap.AllocationModel = "value_requires_review"
			}
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
