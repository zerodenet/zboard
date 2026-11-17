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

func TestFixedTimedQuotaEndsWithoutChangingMonthlyCyclePolicy(t *testing.T) {
	now := time.Now().UTC()
	for _, unit := range []string{"month", "year", "once"} {
		grant, err := NewGrant(GrantRequest{BillingUnit: unit, BillingValue: 1, TrafficBytes: 100}, GrantPolicy{}, now)
		if err != nil || !grant.Subscription.EndsOnQuotaExhaustion {
			t.Fatal(grant, err)
		}
		sub := grant.Subscription
		sub.FlowUsed = 100
		if EffectiveStatus(sub, now) != "expired" || CanChangeAt(sub, now) {
			t.Fatal("terminal service restored", sub)
		}
		for _, operation := range []string{"upgrade", "traffic_pack", "traffic_reset"} {
			if _, err := ApplyGrant(sub, GrantRequest{OrderType: operation, PlanID: sub.PlanID, BillingUnit: "month", BillingValue: 1, TrafficBytes: 100}, GrantPolicy{}, now); err == nil {
				t.Fatal("ended service accepted", operation)
			}
		}
	}
	grant, err := NewGrant(GrantRequest{BillingUnit: "month", BillingValue: 1, TrafficBytes: 100}, GrantPolicy{ResetPolicy: 2}, now)
	if err != nil || grant.Subscription.EndsOnQuotaExhaustion {
		t.Fatal("fixed monthly cycles terminated", grant, err)
	}
}

func TestExplicitCancellationHasNoRenewalDeadline(t *testing.T) {
	now := time.Now().UTC()
	ended := now
	sub := Subscription{Status: "canceled", Lifecycle: "renewable", EndedAt: &ended, EndAt: now.Add(24 * time.Hour), EndReason: "canceled"}
	if CanRenewAt(sub, now) || RenewalDeadline(sub, now) != nil {
		t.Fatal("explicit cancellation entered a recovery window")
	}
}

func TestConfiguredRecoveryWindowsAndTerminalFences(t *testing.T) {
	ended := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	sub := Subscription{ID: 1, UserID: 1, PlanID: 2, Status: "expired", Lifecycle: "renewable", StartAt: ended.AddDate(0, -1, 0), EndAt: ended, EndedAt: &ended, EndReason: "expired", FlowTotal: 100, FlowUsed: 60, ResetQuotaBytes: 100}
	for _, days := range []int{0, 3, 7, 14, 3650} {
		grace := time.Duration(days) * 24 * time.Hour
		deadline := ended.Add(grace)
		got := RenewalDeadline(sub, ended, grace)
		if got == nil || !got.Equal(deadline) || CanRenewAt(sub, deadline, grace) {
			t.Fatal("wrong configured boundary", days, got)
		}
		if days > 0 && !CanRenewAt(sub, deadline.Add(-time.Nanosecond), grace) {
			t.Fatal("window ended early", days)
		}
	}
	grace := 14 * 24 * time.Hour
	now := ended.Add(8 * 24 * time.Hour)
	request := GrantRequest{UserID: 1, PlanID: 2, PlanSKUID: 4, OrderType: "renewal", BillingUnit: "month", BillingValue: 1, RenewalEffect: "extend_only", TrafficBytes: 100}
	policy := GrantPolicy{IsRenewable: true, RecoveryGracePeriod: &grace, ResetPolicy: 1}
	grant, err := ApplyGrant(sub, request, policy, now)
	if err != nil || !grant.Subscription.StartAt.Equal(now) || !grant.Subscription.EndAt.Equal(now.AddDate(0, 1, 0)) {
		t.Fatal("configured recovery did not purchase full term", grant, err)
	}
	for _, status := range []string{"canceled", "ended"} {
		sub.Status = status
		if CanRenewAt(sub, now, 3650*24*time.Hour) || RenewalDeadline(sub, now, 3650*24*time.Hour) != nil {
			t.Fatal("a longer policy resurrected a terminal subscription", status)
		}
	}
}
