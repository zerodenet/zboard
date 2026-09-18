package entitlements

import (
	"errors"
	"time"
)

// GrantRequest is a commercial snapshot accepted by the entitlement owner.
// Payment authorization and idempotent order settlement remain caller duties.
type GrantRequest struct {
	ID                                    uint
	TargetSubscriptionID                  *uint
	UserID, PlanID, PlanSKUID             uint
	OrderType, BillingUnit, RenewalEffect string
	BillingValue                          int
	TrafficBytes                          int64
	SpeedLimitMbps, DeviceLimit           int
}
type GrantPolicy struct {
	MaxActiveSubscriptions       int
	NodeGroupID                  uint
	IsRenewable                  bool
	RenewalPriceMinor            int64
	FamilyLimit                  int
	ResetPolicy, TrafficCalcMode int16
}
type Grant struct {
	Subscription                            Subscription
	QuotaDelta, BalanceBefore, BalanceAfter int64
}

func NewGrant(request GrantRequest, policy GrantPolicy, now time.Time) (Grant, error) {
	end, err := AddBillingPeriod(now, request.BillingUnit, request.BillingValue)
	if err != nil {
		return Grant{}, err
	}
	reset := EffectiveResetPolicy(request.BillingUnit, policy.ResetPolicy)
	renewalPrice := int64(0)
	if policy.IsRenewable {
		renewalPrice = policy.RenewalPriceMinor
	}
	sub := Subscription{
		UserID: request.UserID, PlanID: request.PlanID, PlanSKUID: request.PlanSKUID,
		NodeGroupID: policy.NodeGroupID, SubscriptionType: 1, StartAt: now, EndAt: end, Status: "active",
		FlowTotal: request.TrafficBytes, SpeedLimitMbps: request.SpeedLimitMbps, DeviceLimit: request.DeviceLimit,
		FamilyLimit: policy.FamilyLimit, RenewalPriceMinor: renewalPrice, ResetPolicy: reset,
		NextResetAt: NextTrafficReset(now, reset), TrafficCalcMode: policy.TrafficCalcMode, Config: "{}",
	}
	return Grant{Subscription: sub, QuotaDelta: request.TrafficBytes, BalanceAfter: request.TrafficBytes}, nil
}

func ApplyGrant(sub Subscription, request GrantRequest, policy GrantPolicy, now time.Time) (Grant, error) {
	fulfillment, err := RenewalForGrant(request)
	if err != nil {
		return Grant{}, err
	}
	if !policy.IsRenewable && request.OrderType == "renewal" {
		return Grant{}, errors.New("plan does not support renewal")
	}
	if fulfillment.MakePermanent {
		sub.EndAt = PerpetualEnd
	} else if fulfillment.ExtendPeriod {
		base := sub.EndAt
		if base.Before(now) || (IsPerpetualEnd(base) && request.BillingUnit != "once") {
			base = now
		}
		sub.EndAt, err = AddBillingPeriod(base, request.BillingUnit, request.BillingValue)
		if err != nil {
			return Grant{}, err
		}
	}
	before := sub.FlowTotal - sub.FlowUsed
	delta := int64(0)
	if fulfillment.AddQuota {
		delta = request.TrafficBytes
		sub.FlowTotal += delta
	}
	sub.Status = "active"
	if request.OrderType != "traffic_pack" {
		sub.PlanID = request.PlanID
		sub.PlanSKUID = request.PlanSKUID
		sub.NodeGroupID = policy.NodeGroupID
		sub.SpeedLimitMbps = request.SpeedLimitMbps
		sub.DeviceLimit = request.DeviceLimit
		sub.FamilyLimit = policy.FamilyLimit
		sub.ResetPolicy = EffectiveResetPolicy(request.BillingUnit, policy.ResetPolicy)
		sub.NextResetAt = NextTrafficReset(now, sub.ResetPolicy)
		sub.TrafficCalcMode = policy.TrafficCalcMode
		sub.RenewalPriceMinor = 0
		if policy.IsRenewable {
			sub.RenewalPriceMinor = policy.RenewalPriceMinor
		}
	}
	return Grant{Subscription: sub, QuotaDelta: delta, BalanceBefore: before, BalanceAfter: before + delta}, nil
}
