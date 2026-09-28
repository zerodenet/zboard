package entitlements

import (
	"errors"
	"time"
)

const RenewalGracePeriod = 7 * 24 * time.Hour

var ErrRenewalWindow = errors.New("订阅已结束且不支持续费，或已超过 7 天续费保留期，请新购套餐。")

// Exhausting a timed cycle does not require a reset purchase before changing
// plans. The paid change retains consumption; terminal single-use services
// remain ineligible. Accept the legacy expired marker only for quota exhaustion.
func CanChangeAt(sub Subscription, now time.Time) bool {
	return sub.EndedAt == nil && sub.EndAt.After(now) && !IsPerpetualEnd(sub.EndAt) &&
		(!sub.EndsOnQuotaExhaustion || sub.FlowUsed < sub.FlowTotal) &&
		(sub.Status == "active" || (sub.Status == "expired" && sub.FlowTotal > 0 && sub.FlowUsed >= sub.FlowTotal))
}

// Persist the terminal instant with accounting, so worker delays cannot extend
// the renewal window. Monthly quota exhaustion is not a terminal event.
func RecordQuotaExhaustion(sub Subscription, now time.Time) Subscription {
	if (sub.Status != "active" && sub.Status != "expired") || !sub.EndsOnQuotaExhaustion || sub.FlowUsed < sub.FlowTotal || sub.EndedAt != nil {
		return sub
	}
	ended, reason := now, "exhausted"
	if !sub.EndAt.After(now) {
		ended, reason = sub.EndAt, "expired"
	}
	sub.EndedAt, sub.EndReason, sub.Status = &ended, reason, "expired"
	return sub
}

func RenewalDeadline(sub Subscription, now time.Time) *time.Time {
	if sub.Lifecycle != "renewable" && sub.Lifecycle != "" {
		return nil
	}
	ended := sub.EndedAt
	if ended == nil && !sub.EndAt.After(now) {
		ended = &sub.EndAt
	}
	if ended == nil {
		return nil
	}
	deadline := ended.Add(RenewalGracePeriod)
	return &deadline
}

func CanRenewAt(sub Subscription, now time.Time) bool {
	if sub.Status != "active" && sub.Status != "expired" {
		return false
	}
	if sub.Lifecycle != "renewable" && sub.Lifecycle != "" {
		return false
	}
	if deadline := RenewalDeadline(sub, now); deadline != nil {
		return deadline.After(now)
	}
	return sub.EndAt.After(now)
}
