package metering

import (
	"strings"
	"time"
)

const (
	PolicyScopePlatform     = "platform"
	PolicyScopePlan         = "plan"
	PolicyScopeSubscription = "subscription"

	fairUseEnforcementObserve             = "observe"
	fairUseEnforcementWarn                = "warn"
	fairUseEnforcementRestrict            = "restrict"
	fairUseDefaultEvaluationInterval      = 60
	fairUseMinEvaluationInterval          = 30
	fairUseMaxEvaluationInterval          = 3600
	fairUseMinRestrictionDurationSeconds  = 60
	fairUseMaxRestrictionDurationSeconds  = 7 * 24 * 60 * 60
	fairUseDefaultRestrictionDurationSecs = 60 * 60
)

const (
	fairUseMinConnectionStartWindowSeconds = 10
	fairUseMinWorkingNodeWindowSeconds     = 30
	fairUseMaxTelemetryWindowSeconds       = 3600
)

type Policy struct {
	ScopeType                    string    `json:"scope_type"`
	ScopeID                      uint      `json:"scope_id"`
	Enabled                      bool      `json:"enabled"`
	EvaluationIntervalSeconds    int       `json:"evaluation_interval_seconds"`
	ConnectionStartThreshold     int       `json:"connection_start_threshold"`
	ConnectionStartWindowSeconds int       `json:"connection_start_window_seconds"`
	ConnectionStartPenalty       int       `json:"connection_start_penalty"`
	WorkingNodeThreshold         int       `json:"working_node_threshold"`
	WorkingNodeWindowSeconds     int       `json:"working_node_window_seconds"`
	WorkingNodePenalty           int       `json:"working_node_penalty"`
	ScoreMax                     int       `json:"score_max"`
	RecoveryPerInterval          int       `json:"recovery_per_interval"`
	WarningScore                 int       `json:"warning_score"`
	ViolationScore               int       `json:"violation_score"`
	EnforcementMode              string    `json:"enforcement_mode"`
	RestrictionDurationSeconds   int       `json:"restriction_duration_seconds"`
	Revision                     uint64    `json:"revision"`
	CreatedAt                    time.Time `json:"created_at"`
	UpdatedAt                    time.Time `json:"updated_at"`
}

type PolicySource struct {
	ScopeType string `json:"scope_type"`
	ScopeID   uint   `json:"scope_id"`
}

type PolicyResolution struct {
	Configured bool         `json:"configured"`
	Override   *Policy      `json:"override,omitempty"`
	Effective  Policy       `json:"effective"`
	Source     PolicySource `json:"source"`
}

type PolicyInput struct {
	Enabled                      bool   `json:"enabled"`
	EvaluationIntervalSeconds    int    `json:"evaluation_interval_seconds"`
	ConnectionStartThreshold     int    `json:"connection_start_threshold"`
	ConnectionStartWindowSeconds int    `json:"connection_start_window_seconds"`
	ConnectionStartPenalty       int    `json:"connection_start_penalty"`
	WorkingNodeThreshold         int    `json:"working_node_threshold"`
	WorkingNodeWindowSeconds     int    `json:"working_node_window_seconds"`
	WorkingNodePenalty           int    `json:"working_node_penalty"`
	ScoreMax                     int    `json:"score_max"`
	RecoveryPerInterval          int    `json:"recovery_per_interval"`
	WarningScore                 int    `json:"warning_score"`
	ViolationScore               int    `json:"violation_score"`
	EnforcementMode              string `json:"enforcement_mode"`
	RestrictionDurationSeconds   int    `json:"restriction_duration_seconds"`
	ExpectedRevision             uint64 `json:"expected_revision"`
}

func DefaultPolicy(scopeType string, scopeID uint) Policy {
	return Policy{
		ScopeType:                    scopeType,
		ScopeID:                      scopeID,
		Enabled:                      false,
		EvaluationIntervalSeconds:    fairUseDefaultEvaluationInterval,
		ConnectionStartThreshold:     120,
		ConnectionStartWindowSeconds: 60,
		ConnectionStartPenalty:       10,
		WorkingNodeThreshold:         3,
		WorkingNodeWindowSeconds:     300,
		WorkingNodePenalty:           15,
		ScoreMax:                     100,
		RecoveryPerInterval:          8,
		WarningScore:                 30,
		ViolationScore:               60,
		EnforcementMode:              fairUseEnforcementObserve,
		RestrictionDurationSeconds:   fairUseDefaultRestrictionDurationSecs,
		Revision:                     0,
	}
}

func ValidatePolicy(input PolicyInput) error {
	fields := map[string]string{}
	if input.EvaluationIntervalSeconds < fairUseMinEvaluationInterval || input.EvaluationIntervalSeconds > fairUseMaxEvaluationInterval {
		fields["evaluation_interval_seconds"] = "must be between 30 and 3600"
	}
	if input.ConnectionStartWindowSeconds < fairUseMinConnectionStartWindowSeconds || input.ConnectionStartWindowSeconds > fairUseMaxTelemetryWindowSeconds {
		fields["connection_start_window_seconds"] = "outside supported telemetry range"
	}
	if input.WorkingNodeWindowSeconds < fairUseMinWorkingNodeWindowSeconds || input.WorkingNodeWindowSeconds > fairUseMaxTelemetryWindowSeconds {
		fields["working_node_window_seconds"] = "outside supported telemetry range"
	}
	if input.ConnectionStartThreshold <= 0 || input.ConnectionStartThreshold > 1000000 {
		fields["connection_start_threshold"] = "must be between 1 and 1000000"
	}
	if input.WorkingNodeThreshold <= 0 || input.WorkingNodeThreshold > 10000 {
		fields["working_node_threshold"] = "must be between 1 and 10000"
	}
	if input.ScoreMax < 10 || input.ScoreMax > 10000 {
		fields["score_max"] = "must be between 10 and 10000"
	}
	if input.ConnectionStartPenalty <= 0 || input.ConnectionStartPenalty > input.ScoreMax {
		fields["connection_start_penalty"] = "must be positive and no greater than score_max"
	}
	if input.WorkingNodePenalty <= 0 || input.WorkingNodePenalty > input.ScoreMax {
		fields["working_node_penalty"] = "must be positive and no greater than score_max"
	}
	if input.RecoveryPerInterval <= 0 || input.RecoveryPerInterval > input.ScoreMax {
		fields["recovery_per_interval"] = "must be positive and no greater than score_max"
	}
	if input.WarningScore <= 0 || input.WarningScore >= input.ViolationScore {
		fields["warning_score"] = "must be positive and lower than violation_score"
	}
	if input.ViolationScore <= 0 || input.ViolationScore > input.ScoreMax {
		fields["violation_score"] = "must be positive and no greater than score_max"
	}
	switch strings.ToLower(strings.TrimSpace(input.EnforcementMode)) {
	case fairUseEnforcementObserve, fairUseEnforcementWarn, fairUseEnforcementRestrict:
	default:
		fields["enforcement_mode"] = "must be observe, warn, or restrict"
	}
	if input.RestrictionDurationSeconds < fairUseMinRestrictionDurationSeconds || input.RestrictionDurationSeconds > fairUseMaxRestrictionDurationSeconds {
		fields["restriction_duration_seconds"] = "must be between 60 and 604800"
	}
	if len(fields) > 0 {
		return &PolicyValidation{Fields: fields}
	}
	return nil
}

func PolicyFromInput(scopeType string, scopeID uint, input PolicyInput, revision uint64, now time.Time) Policy {
	return Policy{
		ScopeType:                    scopeType,
		ScopeID:                      scopeID,
		Enabled:                      input.Enabled,
		EvaluationIntervalSeconds:    input.EvaluationIntervalSeconds,
		ConnectionStartThreshold:     input.ConnectionStartThreshold,
		ConnectionStartWindowSeconds: input.ConnectionStartWindowSeconds,
		ConnectionStartPenalty:       input.ConnectionStartPenalty,
		WorkingNodeThreshold:         input.WorkingNodeThreshold,
		WorkingNodeWindowSeconds:     input.WorkingNodeWindowSeconds,
		WorkingNodePenalty:           input.WorkingNodePenalty,
		ScoreMax:                     input.ScoreMax,
		RecoveryPerInterval:          input.RecoveryPerInterval,
		WarningScore:                 input.WarningScore,
		ViolationScore:               input.ViolationScore,
		EnforcementMode:              strings.ToLower(strings.TrimSpace(input.EnforcementMode)),
		RestrictionDurationSeconds:   input.RestrictionDurationSeconds,
		Revision:                     revision,
		UpdatedAt:                    now,
	}
}

func PolicySourceOf(policy Policy) PolicySource {
	return PolicySource{ScopeType: policy.ScopeType, ScopeID: policy.ScopeID}
}

func ChoosePolicy(subscriptionOverride, planOverride, platformOverride *Policy) (Policy, PolicySource) {
	if subscriptionOverride != nil {
		return *subscriptionOverride, PolicySourceOf(*subscriptionOverride)
	}
	if planOverride != nil {
		return *planOverride, PolicySourceOf(*planOverride)
	}
	if platformOverride != nil {
		return *platformOverride, PolicySourceOf(*platformOverride)
	}
	fallback := DefaultPolicy(PolicyScopePlatform, 0)
	return fallback, PolicySourceOf(fallback)
}

type PolicyValidation struct{ Fields map[string]string }

func (*PolicyValidation) Error() string { return "invalid Fair Use policy" }
