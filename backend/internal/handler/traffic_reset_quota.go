package handler

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
)

// Accounting keeps lifetime billed bytes so traffic records remain reconcilable.
// Client-facing quota values describe only the current reset cycle.
func subscriptionCycleQuota(sub model.Subscription) (total, used int64) {
	return entitlements.CycleQuota(entitlements.Subscription(sub))
}
