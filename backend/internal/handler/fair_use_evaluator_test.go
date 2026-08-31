package handler

import (
	"testing"
	"time"
)

func TestDefaultFairUsePolicyRequiresRepeatedAbnormalIntervals(t *testing.T) {
	policy := defaultFairUsePolicy(fairUsePolicyScopePlatform, 0)
	firstScore, firstState, _ := fairUseScoreTransition(policy, 0, 121, 4)
	if firstScore != 25 || firstState != fairUseStateNormal {
		t.Fatalf("first abnormal interval = %d/%s, want 25/normal", firstScore, firstState)
	}
	secondScore, secondState, _ := fairUseScoreTransition(policy, firstScore, 121, 4)
	if secondScore != 50 || secondState != fairUseStateSuspected {
		t.Fatalf("second abnormal interval = %d/%s, want 50/suspected", secondScore, secondState)
	}
	thirdScore, thirdState, _ := fairUseScoreTransition(policy, secondScore, 121, 4)
	if thirdScore != 75 || thirdState != fairUseStateViolated {
		t.Fatalf("third abnormal interval = %d/%s, want 75/violated", thirdScore, thirdState)
	}
}

func TestFairUseScoreRecoversOnNormalIntervals(t *testing.T) {
	policy := defaultFairUsePolicy(fairUsePolicyScopePlatform, 0)
	score, state, _ := fairUseScoreTransition(policy, 50, 10, 1)
	if score != 42 || state != fairUseStateSuspected {
		t.Fatalf("normal recovery = %d/%s, want 42/suspected", score, state)
	}
	for score > 0 {
		score, state, _ = fairUseScoreTransition(policy, score, 10, 1)
	}
	if score != 0 || state != fairUseStateNormal {
		t.Fatalf("final recovery = %d/%s, want 0/normal", score, state)
	}
}

func TestValidateFairUsePolicyRejectsUnsafeRanges(t *testing.T) {
	policy := defaultFairUsePolicy(fairUsePolicyScopePlatform, 0)
	input := fairUsePolicyInput{
		Enabled:                      true,
		EvaluationIntervalSeconds:    policy.EvaluationIntervalSeconds,
		ConnectionStartThreshold:     policy.ConnectionStartThreshold,
		ConnectionStartWindowSeconds: policy.ConnectionStartWindowSeconds,
		ConnectionStartPenalty:       policy.ConnectionStartPenalty,
		WorkingNodeThreshold:         policy.WorkingNodeThreshold,
		WorkingNodeWindowSeconds:     policy.WorkingNodeWindowSeconds,
		WorkingNodePenalty:           policy.WorkingNodePenalty,
		ScoreMax:                     policy.ScoreMax,
		RecoveryPerInterval:          policy.RecoveryPerInterval,
		WarningScore:                 policy.WarningScore,
		ViolationScore:               policy.ViolationScore,
		EnforcementMode:              policy.EnforcementMode,
		RestrictionDurationSeconds:   policy.RestrictionDurationSeconds,
	}
	if err := validateFairUsePolicy(input); err != nil {
		t.Fatalf("default policy should validate: %v", err)
	}
	input.WarningScore = input.ViolationScore
	if err := validateFairUsePolicy(input); err == nil {
		t.Fatal("warning_score >= violation_score must be rejected")
	}
	input.WarningScore = policy.WarningScore
	input.EnforcementMode = "block"
	if err := validateFairUsePolicy(input); err == nil {
		t.Fatal("unknown enforcement mode must be rejected")
	}
}

func TestChooseFairUsePolicyUsesMostSpecificScope(t *testing.T) {
	platform := defaultFairUsePolicy(fairUsePolicyScopePlatform, 0)
	platform.Enabled = true
	plan := platform
	plan.ScopeType = fairUsePolicyScopePlan
	plan.ScopeID = 7
	plan.ConnectionStartThreshold = 80
	subscription := plan
	subscription.ScopeType = fairUsePolicyScopeSubscription
	subscription.ScopeID = 42
	subscription.Enabled = false

	policy, source := chooseFairUsePolicy(&subscription, &plan, &platform)
	if source.ScopeType != fairUsePolicyScopeSubscription || source.ScopeID != 42 || policy.Enabled {
		t.Fatalf("subscription override = %+v / %+v", policy, source)
	}
	policy, source = chooseFairUsePolicy(nil, &plan, &platform)
	if source.ScopeType != fairUsePolicyScopePlan || source.ScopeID != 7 || policy.ConnectionStartThreshold != 80 {
		t.Fatalf("plan override = %+v / %+v", policy, source)
	}
	policy, source = chooseFairUsePolicy(nil, nil, &platform)
	if source.ScopeType != fairUsePolicyScopePlatform || !policy.Enabled {
		t.Fatalf("platform policy = %+v / %+v", policy, source)
	}
}

func TestChooseFairUsePolicyFallsBackToDisabledObserve(t *testing.T) {
	policy, source := chooseFairUsePolicy(nil, nil, nil)
	if policy.Enabled || policy.EnforcementMode != fairUseEnforcementObserve {
		t.Fatalf("fallback policy = %+v", policy)
	}
	if source.ScopeType != fairUsePolicyScopePlatform || source.ScopeID != 0 {
		t.Fatalf("fallback source = %+v", source)
	}
}

func TestParseFairUseResourceIDs(t *testing.T) {
	id, err := parseFairUseResourceSubscriptionID("/api/v1/admin/subscriptions/99/fair-use/state", "/fair-use/state")
	if err != nil || id != 99 {
		t.Fatalf("parse state path = %d, %v", id, err)
	}
	planID, err := parseFairUseResourcePlanID("/api/v1/admin/plans/7/fair-use/policy", "/fair-use/policy")
	if err != nil || planID != 7 {
		t.Fatalf("parse plan path = %d, %v", planID, err)
	}
	for _, path := range []string{
		"/api/v1/admin/subscriptions/0/fair-use/state",
		"/api/v1/admin/subscriptions/99/fair-use/policy",
		"/api/v1/admin/subscriptions/not-number/fair-use/state",
	} {
		if _, err := parseFairUseResourceSubscriptionID(path, "/fair-use/state"); err == nil {
			t.Fatalf("path %q should fail", path)
		}
	}
}

func TestFairUseEvaluationIsDueBeforeTelemetryWork(t *testing.T) {
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	recent := now.Add(-29 * time.Second)
	due := now.Add(-30 * time.Second)
	if fairUseEvaluationIsDue(&recent, 30, now) {
		t.Fatal("an evaluation inside its interval must be skipped before telemetry queries")
	}
	if !fairUseEvaluationIsDue(&due, 30, now) || !fairUseEvaluationIsDue(nil, 30, now) {
		t.Fatal("a due or never-evaluated subscription must be selected")
	}
}
