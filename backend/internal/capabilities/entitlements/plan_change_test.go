package entitlements

import (
	"testing"
	"time"
)

func TestPlanChangeReplacesCycleQuotaAndPreservesPeriodAndUsage(t *testing.T) {
	const gb = int64(1024 * 1024 * 1024)
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name           string
		baseline, used int64
	}{
		{"unused", 0, 0}, {"partially used", 0, 40 * gb}, {"after a reset", 20 * gb, 40 * gb},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub := Subscription{ID: 1, UserID: 1, PlanID: 1, Status: "active", StartAt: now.Add(-24 * time.Hour), EndAt: now.Add(29 * 24 * time.Hour), FlowTotal: tc.baseline + 100*gb, FlowUsed: tc.baseline + tc.used, CycleStartUsed: tc.baseline}
			request := GrantRequest{OrderType: "upgrade", PlanID: 2, PlanSKUID: 2, BillingUnit: "month", BillingValue: 1, TrafficBytes: 150 * gb, DeviceLimit: 3, SpeedLimitMbps: 50}
			grant, err := ApplyGrant(sub, request, GrantPolicy{NodeGroupID: 2}, now)
			if err != nil {
				t.Fatal(err)
			}
			got := grant.Subscription
			total, used := CycleQuota(got)
			if total != 150*gb || used != tc.used || got.FlowUsed != sub.FlowUsed || got.CycleStartUsed != sub.CycleStartUsed {
				t.Fatal("plan change added quota or erased usage", got)
			}
			if !got.StartAt.Equal(sub.StartAt) || !got.EndAt.Equal(sub.EndAt) || got.PlanID != 2 || got.ResetQuotaBytes != 150*gb {
				t.Fatal("plan change extended period or lost new plan terms", got)
			}
			if grant.QuotaDelta != 50*gb || grant.BalanceAfter != 150*gb-tc.used {
				t.Fatal("incorrect replacement ledger", grant)
			}
		})
	}
}

func TestPlanChangeRejectsExpiredTargetOrQuotaBelowUsage(t *testing.T) {
	now := time.Now().UTC()
	sub := Subscription{Status: "active", EndAt: now.Add(time.Hour), FlowTotal: 100, FlowUsed: 60}
	for _, next := range []Subscription{sub, {Status: "expired", EndAt: now.Add(time.Hour)}, {Status: "active", EndAt: now}} {
		if _, err := ApplyGrant(next, GrantRequest{OrderType: "upgrade", TrafficBytes: 50}, GrantPolicy{}, now); err == nil {
			t.Fatal("unavailable target accepted", next)
		}
	}
}
