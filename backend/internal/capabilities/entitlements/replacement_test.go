package entitlements

import (
	"math"
	"testing"
	"time"
)

func TestFullReplacementStartsCompleteTermAndQuotaWithoutOldRemainders(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	reset := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	old := Subscription{ID: 77, UserID: 8, PlanID: 1, PlanSKUID: 10, NodeGroupID: 1, Lifecycle: "renewable", Status: "active", StartAt: now.AddDate(0, -3, 0), EndAt: now.AddDate(0, 9, 0), FlowUsed: 107, FlowTotal: 200, CycleStartUsed: 100, ResetQuotaBytes: 100, ResetPolicy: 1, NextResetAt: &reset, CreatedAt: now.AddDate(-1, 0, 0), Config: `{"label":"independent"}`, KernelPolicyRevision: 7, EntitlementEpoch: 2}
	request := GrantRequest{ID: 90, UserID: 8, TargetSubscriptionID: &old.ID, PlanID: 2, PlanSKUID: 20, OrderType: "upgrade", BillingUnit: "month", BillingValue: 1, TrafficBytes: 5, DeviceLimit: 2, ReplaceTerm: true}
	policy := GrantPolicy{NodeGroupID: 2, IsRenewable: true, RenewalPriceMinor: 2000, ResetPolicy: 1, TrafficCalcMode: 2}
	got, err := ApplyGrant(old, request, policy, now)
	if err != nil {
		t.Fatal(err)
	}
	sub := got.Subscription
	total, used := CycleQuota(sub)
	if sub.ID != old.ID || sub.UserID != old.UserID || !sub.StartAt.Equal(now) || !sub.EndAt.Equal(now.AddDate(0, 1, 0)) || sub.PlanID != 2 || sub.NodeGroupID != 2 || total != 5 || used != 0 || sub.FlowUsed != 107 || sub.CycleStartUsed != 107 || got.BalanceBefore != 93 || got.BalanceAfter != 5 || got.QuotaDelta != -88 {
		t.Fatalf("full replacement carried old rights: grant=%+v total=%d used=%d", got, total, used)
	}
	if sub.RenewalPriceMinor != 2000 || sub.ResetQuotaBytes != 5 || sub.NextResetAt == nil || !sub.NextResetAt.Equal(reset) || sub.TrafficCalcMode != 2 || sub.Config != old.Config || !sub.CreatedAt.Equal(old.CreatedAt) || sub.KernelPolicyRevision != 7 {
		t.Fatal("new terms or identity metadata were lost", sub)
	}
	// Legacy already-agreed change semantics remain explicit until commerce
	// selects the versioned new checkout; a caller cannot infer it from price.
	request.ReplaceTerm = false
	if _, err := ApplyGrant(old, request, policy, now); err == nil {
		t.Fatal("legacy change unexpectedly bypassed current-cycle quota protection")
	}
}
func TestFullReplacementRejectsEndedForeignAndOverflowedSources(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	valid := Subscription{ID: 1, UserID: 1, Status: "active", StartAt: now.AddDate(0, -1, 0), EndAt: now.AddDate(0, 1, 0), FlowTotal: 100, FlowUsed: 40}
	request := GrantRequest{UserID: 1, OrderType: "upgrade", BillingUnit: "month", BillingValue: 1, TrafficBytes: 100, ReplaceTerm: true}
	for _, name := range []string{"canceled", "elapsed", "foreign", "overflow"} {
		t.Run(name, func(t *testing.T) {
			sub := valid
			in := request
			switch name {
			case "canceled":
				sub.Status = "canceled"
			case "elapsed":
				sub.EndAt = now
			case "foreign":
				in.UserID = 2
			case "overflow":
				sub.FlowUsed = math.MaxInt64
				sub.FlowTotal = math.MaxInt64
			}
			if _, err := ApplyGrant(sub, in, GrantPolicy{IsRenewable: true}, now); err == nil {
				t.Fatal("invalid replacement accepted")
			}
		})
	}
}
