package entitlements

import (
	"errors"
	"math"
	"time"
)

// A full replacement purchases a complete new term. Old time/quota must have
// been settled by commerce; this grant never carries them into the new service.
// The cumulative counter remains an internal baseline, not spendable quota.
func replacementGrant(previous Subscription, request GrantRequest, policy GrantPolicy, now time.Time) (Grant, error) {
	if request.OrderType != "upgrade" || !CanReplaceAt(previous, now) || request.UserID != previous.UserID || (request.TargetSubscriptionID != nil && *request.TargetSubscriptionID != previous.ID) {
		return Grant{}, errors.New("full replacement requires an eligible owned subscription")
	}
	if previous.FlowUsed < 0 || request.TrafficBytes < 0 || previous.FlowUsed > math.MaxInt64-request.TrafficBytes {
		return Grant{}, errors.New("replacement quota exceeds supported range")
	}
	fresh, err := NewGrant(request, policy, now)
	if err != nil {
		return Grant{}, err
	}
	sub := fresh.Subscription
	sub.ID, sub.UserID, sub.SubscriptionType = previous.ID, previous.UserID, previous.SubscriptionType
	sub.Config, sub.CreatedAt, sub.UpdatedAt = previous.Config, previous.CreatedAt, now
	sub.EntitlementEpoch = previous.EntitlementEpoch
	sub.KernelPolicyRevision, sub.KernelPolicyFingerprint = previous.KernelPolicyRevision, previous.KernelPolicyFingerprint
	sub.FlowUsed, sub.CycleStartUsed = previous.FlowUsed, previous.FlowUsed
	sub.FlowTotal = previous.FlowUsed + request.TrafficBytes
	before := max(int64(0), previous.FlowTotal-previous.FlowUsed)
	return Grant{Subscription: sub, QuotaDelta: request.TrafficBytes - before, BalanceBefore: before, BalanceAfter: request.TrafficBytes}, nil
}
