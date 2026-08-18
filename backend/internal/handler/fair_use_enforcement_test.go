package handler

import (
	"testing"
	"time"
)

func TestFairUseRestrictionActiveRequiresFutureDeadline(t *testing.T) {
	now := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Second)
	if !fairUseRestrictionActive(subscriptionFairUseRestriction{Active: true, RestrictedUntil: &future}, now) {
		t.Fatal("active future restriction must be effective")
	}
	if fairUseRestrictionActive(subscriptionFairUseRestriction{Active: true, RestrictedUntil: &past}, now) {
		t.Fatal("expired restriction must fail open even before cleanup")
	}
	if fairUseRestrictionActive(subscriptionFairUseRestriction{Active: false, RestrictedUntil: &future}, now) {
		t.Fatal("released restriction must not be effective")
	}
}

func TestFairUseAutomaticRestrictionRequiresFreshCompleteViolation(t *testing.T) {
	now := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	policy := defaultFairUsePolicy(fairUsePolicyScopePlatform, 0)
	policy.Enabled = true
	policy.EnforcementMode = fairUseEnforcementRestrict
	complete := now.Add(-30 * time.Second)
	state := subscriptionFairUseState{
		State:                 fairUseStateViolated,
		TelemetryCompleteness: "complete",
		LastCompleteAt:        &complete,
		LastEvaluatedAt:       &complete,
	}
	if !fairUseAutomaticRestrictionEligible(policy, state, nil, now) {
		t.Fatal("fresh complete violated evaluation should be eligible")
	}
	incomplete := state
	incomplete.TelemetryCompleteness = "incomplete"
	if fairUseAutomaticRestrictionEligible(policy, incomplete, nil, now) {
		t.Fatal("incomplete telemetry must never trigger restriction")
	}
	stale := now.Add(-3 * time.Minute)
	state.LastCompleteAt = &stale
	state.LastEvaluatedAt = &stale
	if fairUseAutomaticRestrictionEligible(policy, state, nil, now) {
		t.Fatal("stale complete evaluation must not trigger after restart")
	}
}

func TestFairUseAutomaticRestrictionRespectsHoldAndNewEvaluation(t *testing.T) {
	now := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	policy := defaultFairUsePolicy(fairUsePolicyScopePlatform, 0)
	policy.Enabled = true
	policy.EnforcementMode = fairUseEnforcementRestrict
	complete := now.Add(-10 * time.Second)
	state := subscriptionFairUseState{State: fairUseStateViolated, TelemetryCompleteness: "complete", LastCompleteAt: &complete, LastEvaluatedAt: &complete}
	hold := now.Add(time.Minute)
	previous := complete.Add(-time.Minute)
	row := &subscriptionFairUseRestriction{HoldUntil: &hold, LastSourceEvaluationAt: &previous}
	if fairUseAutomaticRestrictionEligible(policy, state, row, now) {
		t.Fatal("post-release hold must prevent immediate re-restriction")
	}
	row.HoldUntil = nil
	row.LastSourceEvaluationAt = &complete
	if fairUseAutomaticRestrictionEligible(policy, state, row, now) {
		t.Fatal("same evaluation must not be reused for a new restriction")
	}
	newer := complete.Add(time.Second)
	state.LastCompleteAt = &newer
	state.LastEvaluatedAt = &newer
	if !fairUseAutomaticRestrictionEligible(policy, state, row, now) {
		t.Fatal("new complete evaluation after hold should be eligible")
	}
}
