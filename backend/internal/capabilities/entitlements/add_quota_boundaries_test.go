package entitlements

import (
	"math"
	"testing"
	"time"
)

func TestAdditionalQuotaCannotOverflowOrReduceAnEntitlement(t *testing.T) {
	now := time.Now().UTC()
	sub := Subscription{ID: 1, UserID: 1, PlanID: 1, PlanSKUID: 1, Status: "active", StartAt: now.Add(-time.Hour), EndAt: PerpetualEnd, FlowTotal: math.MaxInt64 - 10, FlowUsed: 20, ResetPolicy: 5}
	for _, amount := range []int64{-1, 11, math.MaxInt64} {
		if _, err := ApplyGrant(sub, GrantRequest{OrderType: "traffic_pack", TrafficBytes: amount}, GrantPolicy{}, now); err == nil {
			t.Fatalf("invalid grant accepted: %d", amount)
		}
	}
	granted, err := ApplyGrant(sub, GrantRequest{OrderType: "traffic_pack", TrafficBytes: 10}, GrantPolicy{}, now)
	if err != nil || granted.Subscription.FlowTotal != math.MaxInt64 || granted.Subscription.FlowUsed != 20 {
		t.Fatalf("exact integer boundary %+v %v", granted, err)
	}
}
