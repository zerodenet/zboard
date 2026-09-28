package entitlements

import (
	"testing"
	"time"
)

func TestPurchasedTrafficResetRestoresQuotaAndKeepsPlanAndSchedule(t *testing.T) {
	now := time.Now().UTC()
	next := now.Add(10 * 24 * time.Hour)
	for _, used := range []int64{80, 100} {
		sub := Subscription{ID: 1, UserID: 1, PlanID: 1, PlanSKUID: 1, NodeGroupID: 1, Status: "expired", StartAt: now.Add(-15 * 24 * time.Hour), EndAt: now.Add(15 * 24 * time.Hour), FlowTotal: 100, FlowUsed: used, ResetQuotaBytes: 100, ResetPolicy: 2, NextResetAt: &next, SpeedLimitMbps: 10, DeviceLimit: 3}
		grant, err := ApplyGrant(sub, GrantRequest{OrderType: "traffic_reset", PlanID: 1, PlanSKUID: 2, BillingUnit: "once", BillingValue: 1, TrafficBytes: 100}, GrantPolicy{NodeGroupID: 9, ResetPolicy: 1}, now)
		if err != nil {
			t.Fatal(err)
		}
		got := grant.Subscription
		total, cycleUsed := CycleQuota(got)
		if total != 100 || cycleUsed != 0 || got.FlowUsed != used || got.FlowTotal != used+100 || got.CycleStartUsed != used || got.Status != "active" {
			t.Fatal("reset lost counters or added leftover quota", got)
		}
		if got.PlanSKUID != sub.PlanSKUID || got.PlanID != sub.PlanID || got.NodeGroupID != sub.NodeGroupID || !got.EndAt.Equal(sub.EndAt) || !got.StartAt.Equal(sub.StartAt) || !got.NextResetAt.Equal(next) || got.ResetPolicy != 2 || got.DeviceLimit != 3 || got.SpeedLimitMbps != 10 {
			t.Fatal("reset changed subscription terms", got)
		}
	}
}
func TestPurchasedTrafficResetRejectsExpiredCanceledPermanentAndWrongPlan(t *testing.T) {
	now := time.Now().UTC()
	sub := Subscription{PlanID: 1, Status: "active", EndAt: now.Add(time.Hour), ResetQuotaBytes: 100}
	for _, mutate := range []func(*Subscription){
		func(s *Subscription) { s.EndAt = now }, func(s *Subscription) { s.Status = "canceled" }, func(s *Subscription) { s.EndAt = PerpetualEnd }, func(s *Subscription) { s.PlanID = 2 },
	} {
		target := sub
		mutate(&target)
		if _, err := ApplyGrant(target, GrantRequest{OrderType: "traffic_reset", PlanID: 1, TrafficBytes: 100}, GrantPolicy{}, now); err == nil {
			t.Fatal("invalid reset accepted", target)
		}
	}
}
