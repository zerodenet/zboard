package entitlements

import (
	"errors"
	"testing"
	"time"
)

func TestRenewalGraceEndsAtSevenDaysAndDoesNotRestoreCanceledServices(t *testing.T) {
	ended := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	sub := Subscription{PlanID: 1, Lifecycle: "renewable", Status: "expired", EndAt: ended, EndedAt: &ended, FlowTotal: 100, FlowUsed: 80}
	deadline := ended.Add(RenewalGracePeriod)
	if !CanRenewAt(sub, deadline.Add(-time.Nanosecond)) || CanRenewAt(sub, deadline) {
		t.Fatal("seven day boundary")
	}
	sub.Status = "canceled"
	if CanRenewAt(sub, ended.Add(time.Hour)) {
		t.Fatal("canceled subscription recovered")
	}
	sub.Status, sub.Lifecycle = "expired", "fixed"
	if CanRenewAt(sub, ended.Add(time.Hour)) {
		t.Fatal("nonrenewable service recovered")
	}
}

func TestPlanChangeAllowsCycleExhaustionButNotTerminalSubscriptions(t *testing.T) {
	now := time.Now().UTC()
	sub := Subscription{Status: "active", EndAt: now.Add(time.Hour), FlowTotal: 100, FlowUsed: 100}
	if !CanChangeAt(sub, now) {
		t.Fatal("exhausted cycle incorrectly requires a reset purchase")
	}
	sub.Status = "expired"
	if !CanChangeAt(sub, now) {
		t.Fatal("legacy exhausted cycle cannot change")
	}
	for _, mutate := range []func(*Subscription){
		func(s *Subscription) { s.EndsOnQuotaExhaustion = true },
		func(s *Subscription) { s.EndedAt = &now },
		func(s *Subscription) { s.EndAt = now },
		func(s *Subscription) { s.EndAt = PerpetualEnd },
		func(s *Subscription) { s.Status = "canceled" },
		func(s *Subscription) { s.FlowUsed = 50 }, // expired for a reason other than exhaustion
	} {
		invalid := sub
		mutate(&invalid)
		if CanChangeAt(invalid, now) {
			t.Fatal("unavailable service accepted", invalid)
		}
	}
}

func TestGraceRenewalStartsFreshPeriodAndQuotaWhilePreservingRawConsumption(t *testing.T) {
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	ended := now.Add(-time.Hour)
	sub := Subscription{ID: 1, PlanID: 1, Lifecycle: "renewable", Status: "expired", EndAt: ended, EndedAt: &ended, EndReason: "expired", FlowTotal: 100, FlowUsed: 80, ResetQuotaBytes: 100}
	request := GrantRequest{PlanID: 1, PlanSKUID: 2, OrderType: "renewal", BillingUnit: "month", BillingValue: 1, RenewalEffect: "extend_only", TrafficBytes: 100}
	policy := GrantPolicy{IsRenewable: true, ResetPolicy: 2}
	grant, err := ApplyGrant(sub, request, policy, now)
	if err != nil {
		t.Fatal(err)
	}
	got := grant.Subscription
	total, used := CycleQuota(got)
	if total != 100 || used != 0 || got.FlowUsed != 80 || got.EndedAt != nil || got.EndReason != "" || got.Status != "active" || !got.EndAt.After(now) || got.NextResetAt == nil || !got.NextResetAt.After(now) {
		t.Fatalf("recovery: %+v", got)
	}
	if _, err := ApplyGrant(sub, request, policy, ended.Add(RenewalGracePeriod)); !errors.Is(err, ErrRenewalWindow) {
		t.Fatal("late fulfillment", err)
	}
	request.RenewalEffect = "add_quota_only"
	grant, err = ApplyGrant(sub, request, policy, now)
	if err != nil || !grant.Subscription.EndAt.After(now) {
		t.Fatal("recovery left service expired", grant, err)
	}
	request.RenewalEffect = "extend_only"
	// An ordinary early renewal still obeys the SKU's extend-only policy.
	sub.Status, sub.EndAt, sub.EndedAt = "active", now.Add(time.Hour), nil
	grant, err = ApplyGrant(sub, request, policy, now)
	if err != nil || grant.Subscription.FlowTotal != 100 || grant.Subscription.FlowUsed != 80 {
		t.Fatal("early renewal changed", grant, err)
	}
}

func TestSingleUseAndRenewabilityAreIndependentGrantPolicies(t *testing.T) {
	now := time.Now().UTC()
	for _, renewable := range []bool{false, true} {
		grant, err := NewGrant(GrantRequest{BillingUnit: "once", BillingValue: 1, TrafficBytes: 100}, GrantPolicy{IsRenewable: renewable}, now)
		if err != nil || !grant.Subscription.EndsOnQuotaExhaustion {
			t.Fatal(grant, err)
		}
		if (grant.Subscription.Lifecycle == "renewable") != renewable {
			t.Fatal("renewability not captured", grant)
		}
	}
	grant, err := NewGrant(GrantRequest{BillingUnit: "month", BillingValue: 1, TrafficBytes: 100}, GrantPolicy{IsRenewable: true, ResetPolicy: 2}, now)
	if err != nil || grant.Subscription.EndsOnQuotaExhaustion {
		t.Fatal("monthly quota exhaustion must not end subscription", grant, err)
	}
}

func TestQuotaTerminationRecordsFirstAccountingInstantAndMonthlyServiceStaysActive(t *testing.T) {
	now := time.Now().UTC()
	sub := Subscription{Status: "active", Lifecycle: "renewable", EndsOnQuotaExhaustion: true, EndAt: PerpetualEnd, FlowTotal: 100, FlowUsed: 100}
	ended := RecordQuotaExhaustion(sub, now)
	if ended.EndedAt == nil || !ended.EndedAt.Equal(now) || ended.Status != "expired" || ended.EndReason != "exhausted" {
		t.Fatal(ended)
	}
	later := RecordQuotaExhaustion(ended, now.Add(time.Hour))
	if !later.EndedAt.Equal(now) || CanRenewAt(later, now.Add(RenewalGracePeriod)) {
		t.Fatal("replay moved renewal deadline", later)
	}
	sub.EndsOnQuotaExhaustion = false
	monthly := RecordQuotaExhaustion(sub, now)
	if monthly.EndedAt != nil || monthly.Status != "active" {
		t.Fatal("monthly service ended", monthly)
	}
	sub.EndsOnQuotaExhaustion, sub.Status = true, "canceled"
	canceled := RecordQuotaExhaustion(sub, now)
	if canceled.Status != "canceled" || CanRenewAt(canceled, now) {
		t.Fatal("late traffic made canceled service renewable", canceled)
	}
}
