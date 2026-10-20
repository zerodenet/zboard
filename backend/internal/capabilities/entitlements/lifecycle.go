package entitlements

import (
	"errors"
	"time"
)

const RenewalGracePeriod = 7 * 24 * time.Hour

var ErrGrantTargetRequired = errors.New("历史订单缺少可核对的独立订阅目标，请通过工单核对")

var ErrRenewalWindow = errors.New("订阅已结束且不支持续费，或已超过续费保留期，请新购套餐。")

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

func RenewalDeadline(sub Subscription, now time.Time, configuredGrace ...time.Duration) *time.Time {
	grace := RenewalGracePeriod
	if len(configuredGrace) > 0 {
		grace = configuredGrace[0]
	}
	if grace < 0 {
		return nil
	}
	if sub.Status == "canceled" || sub.Status == "ended" || sub.EndReason == "canceled" {
		return nil
	}
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
	deadline := ended.Add(grace)
	return &deadline
}

func CanRenewAt(sub Subscription, now time.Time, configuredGrace ...time.Duration) bool {
	if len(configuredGrace) > 0 && configuredGrace[0] < 0 {
		return false
	}
	if sub.Status != "active" && sub.Status != "expired" {
		return false
	}
	if sub.Lifecycle != "renewable" && sub.Lifecycle != "" {
		return false
	}
	if deadline := RenewalDeadline(sub, now, configuredGrace...); deadline != nil {
		return deadline.After(now)
	}
	return sub.EndAt.After(now)
}
