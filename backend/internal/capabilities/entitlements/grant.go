package entitlements

import (
	"errors"
	"math"
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
	lifecycle := "fixed"
	if policy.IsRenewable {
		lifecycle = "renewable"
	}
	sub := Subscription{
		Lifecycle: lifecycle, EndsOnQuotaExhaustion: quotaEndsService(request.BillingUnit, policy.IsRenewable, reset),
		UserID: request.UserID, PlanID: request.PlanID, PlanSKUID: request.PlanSKUID,
		NodeGroupID: policy.NodeGroupID, SubscriptionType: 1, StartAt: now, EndAt: end, Status: "active",
		FlowTotal: request.TrafficBytes, ResetQuotaBytes: request.TrafficBytes, SpeedLimitMbps: request.SpeedLimitMbps, DeviceLimit: request.DeviceLimit,
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
	recovering := request.OrderType == "renewal" && (sub.EndedAt != nil || !sub.EndAt.After(now))
	if request.OrderType == "renewal" && (!CanRenewAt(sub, now) || sub.PlanID != request.PlanID) {
		return Grant{}, ErrRenewalWindow
	}
	if request.OrderType != "renewal" && (sub.EndedAt != nil || !sub.EndAt.After(now) || (sub.EndsOnQuotaExhaustion && sub.FlowUsed >= sub.FlowTotal)) {
		return Grant{}, errors.New("subscription has ended")
	}
	if fulfillment.MakePermanent {
		sub.EndAt = PerpetualEnd
	} else if fulfillment.ExtendPeriod || recovering {
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
	if request.OrderType == "traffic_reset" {
		if sub.PlanID != request.PlanID || (sub.Status != "active" && sub.Status != "expired") || !sub.EndAt.After(now) || IsPerpetualEnd(sub.EndAt) || request.TrafficBytes <= 0 || request.TrafficBytes != sub.ResetQuotaBytes || sub.FlowUsed > math.MaxInt64-request.TrafficBytes {
			return Grant{}, errors.New("traffic reset requires a valid timed subscription and its base quota")
		}
		previousTotal := sub.FlowTotal
		sub.FlowTotal = sub.FlowUsed + request.TrafficBytes
		sub.CycleStartUsed = sub.FlowUsed
		delta = sub.FlowTotal - previousTotal
	} else if request.OrderType == "upgrade" {
		if !CanChangeAt(sub, now) {
			return Grant{}, errors.New("plan change requires an active subscription")
		}
		_, cycleUsed := CycleQuota(sub)
		baseline := sub.FlowUsed - cycleUsed
		if request.TrafficBytes < cycleUsed || request.TrafficBytes > math.MaxInt64-baseline {
			return Grant{}, errors.New("new plan quota cannot cover current-cycle usage")
		}
		previousTotal := sub.FlowTotal
		sub.FlowTotal = baseline + request.TrafficBytes
		delta = sub.FlowTotal - previousTotal
	} else if recovering {
		if request.TrafficBytes <= 0 || sub.FlowUsed > math.MaxInt64-request.TrafficBytes {
			return Grant{}, errors.New("invalid renewal recovery quota")
		}
		previousTotal := sub.FlowTotal
		sub.CycleStartUsed = sub.FlowUsed
		sub.FlowTotal = sub.FlowUsed + request.TrafficBytes
		delta = sub.FlowTotal - previousTotal
		sub.StartAt = now
		sub.EndedAt, sub.EndReason = nil, ""
	} else if fulfillment.AddQuota {
		delta = request.TrafficBytes
		sub.FlowTotal += delta
	}
	sub.Status = "active"
	if request.OrderType != "traffic_pack" && request.OrderType != "traffic_reset" {
		sub.Lifecycle = "fixed"
		if policy.IsRenewable {
			sub.Lifecycle = "renewable"
		}
		sub.EndsOnQuotaExhaustion = quotaEndsService(request.BillingUnit, policy.IsRenewable, EffectiveResetPolicy(request.BillingUnit, policy.ResetPolicy))
		sub.PlanID = request.PlanID
		sub.PlanSKUID = request.PlanSKUID
		sub.NodeGroupID = policy.NodeGroupID
		sub.SpeedLimitMbps = request.SpeedLimitMbps
		sub.DeviceLimit = request.DeviceLimit
		sub.FamilyLimit = policy.FamilyLimit
		if request.TrafficBytes > 0 {
			sub.ResetQuotaBytes = request.TrafficBytes
		}
		previousResetPolicy := sub.ResetPolicy
		sub.ResetPolicy = EffectiveResetPolicy(request.BillingUnit, policy.ResetPolicy)
		if sub.ResetPolicy != previousResetPolicy || sub.NextResetAt == nil || sub.NextResetAt.After(now) {
			sub.NextResetAt = NextTrafficResetAfter(sub.StartAt, sub.ResetPolicy, now)
		}
		if recovering {
			sub.NextResetAt = NextTrafficReset(now, sub.ResetPolicy)
		}
		sub.TrafficCalcMode = policy.TrafficCalcMode
		sub.RenewalPriceMinor = 0
		if policy.IsRenewable {
			sub.RenewalPriceMinor = policy.RenewalPriceMinor
		}
	}
	return Grant{Subscription: sub, QuotaDelta: delta, BalanceBefore: before, BalanceAfter: before + delta}, nil
}

// Fixed quota services have no future cycle to restore their exhausted quota.
// A sale's validity period can still be expressed in months or years.
func quotaEndsService(billingUnit string, renewable bool, reset int16) bool {
	return (billingUnit == "once" || !renewable) && (reset == 0 || reset == 5)
}
