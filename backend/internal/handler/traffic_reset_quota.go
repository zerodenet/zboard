package handler

import "github.com/zerodenet/zboard/backend/internal/model"

// Accounting keeps lifetime billed bytes so traffic records remain reconcilable.
// Client-facing quota values describe only the current reset cycle.
func subscriptionCycleQuota(sub model.Subscription) (total, used int64) {
	baseline := sub.CycleStartUsed
	if baseline < 0 || baseline > sub.FlowUsed || baseline > sub.FlowTotal {
		baseline = 0
	}
	return sub.FlowTotal - baseline, sub.FlowUsed - baseline
}
