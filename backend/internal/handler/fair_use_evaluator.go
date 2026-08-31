package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	fairUseStateNormal    = "normal"
	fairUseStateSuspected = "suspected"
	fairUseStateViolated  = "violated"

	fairUsePolicyScopePlatform     = "platform"
	fairUsePolicyScopePlan         = "plan"
	fairUsePolicyScopeSubscription = "subscription"

	fairUseEnforcementObserve  = "observe"
	fairUseEnforcementWarn     = "warn"
	fairUseEnforcementRestrict = "restrict"

	fairUseEvaluationWorkerInterval       = 15 * time.Second
	fairUseEvaluationBatchSize            = 100
	fairUseDefaultEvaluationInterval      = 60
	fairUseMinEvaluationInterval          = 30
	fairUseMaxEvaluationInterval          = 3600
	fairUseMinRestrictionDurationSeconds  = 60
	fairUseMaxRestrictionDurationSeconds  = 7 * 24 * 60 * 60
	fairUseDefaultRestrictionDurationSecs = 60 * 60
)

type fairUsePolicy struct {
	ScopeType                    string    `json:"scope_type" gorm:"column:scope_type;primaryKey"`
	ScopeID                      uint      `json:"scope_id" gorm:"column:scope_id;primaryKey"`
	Enabled                      bool      `json:"enabled" gorm:"column:enabled"`
	EvaluationIntervalSeconds    int       `json:"evaluation_interval_seconds" gorm:"column:evaluation_interval_seconds"`
	ConnectionStartThreshold     int       `json:"connection_start_threshold" gorm:"column:connection_start_threshold"`
	ConnectionStartWindowSeconds int       `json:"connection_start_window_seconds" gorm:"column:connection_start_window_seconds"`
	ConnectionStartPenalty       int       `json:"connection_start_penalty" gorm:"column:connection_start_penalty"`
	WorkingNodeThreshold         int       `json:"working_node_threshold" gorm:"column:working_node_threshold"`
	WorkingNodeWindowSeconds     int       `json:"working_node_window_seconds" gorm:"column:working_node_window_seconds"`
	WorkingNodePenalty           int       `json:"working_node_penalty" gorm:"column:working_node_penalty"`
	ScoreMax                     int       `json:"score_max" gorm:"column:score_max"`
	RecoveryPerInterval          int       `json:"recovery_per_interval" gorm:"column:recovery_per_interval"`
	WarningScore                 int       `json:"warning_score" gorm:"column:warning_score"`
	ViolationScore               int       `json:"violation_score" gorm:"column:violation_score"`
	EnforcementMode              string    `json:"enforcement_mode" gorm:"column:enforcement_mode"`
	RestrictionDurationSeconds   int       `json:"restriction_duration_seconds" gorm:"column:restriction_duration_seconds"`
	Revision                     uint64    `json:"revision" gorm:"column:revision"`
	CreatedAt                    time.Time `json:"created_at" gorm:"column:created_at"`
	UpdatedAt                    time.Time `json:"updated_at" gorm:"column:updated_at"`
}

func (fairUsePolicy) TableName() string { return "fair_use_policies" }

type fairUsePolicySource struct {
	ScopeType string `json:"scope_type"`
	ScopeID   uint   `json:"scope_id"`
}

type fairUsePolicyResolution struct {
	Configured bool                `json:"configured"`
	Override   *fairUsePolicy      `json:"override,omitempty"`
	Effective  fairUsePolicy       `json:"effective"`
	Source     fairUsePolicySource `json:"source"`
}

type subscriptionFairUseState struct {
	SubscriptionID        uint       `json:"subscription_id" gorm:"column:subscription_id;primaryKey"`
	Score                 int        `json:"score" gorm:"column:score"`
	State                 string     `json:"state" gorm:"column:state"`
	CurrentActiveFlows    *uint64    `json:"current_active_flows" gorm:"column:current_active_flows"`
	ConnectionStarts      int        `json:"connection_starts" gorm:"column:connection_starts"`
	WorkingNodes          int        `json:"working_nodes" gorm:"column:working_nodes"`
	TelemetryCompleteness string     `json:"telemetry_completeness" gorm:"column:telemetry_completeness"`
	LastEvaluatedAt       *time.Time `json:"last_evaluated_at,omitempty" gorm:"column:last_evaluated_at"`
	LastCompleteAt        *time.Time `json:"last_complete_at,omitempty" gorm:"column:last_complete_at"`
	CreatedAt             time.Time  `json:"created_at" gorm:"column:created_at"`
	UpdatedAt             time.Time  `json:"updated_at" gorm:"column:updated_at"`
}

func (subscriptionFairUseState) TableName() string { return "subscription_fair_use_states" }

type subscriptionFairUseEvent struct {
	ID             uint64    `json:"id" gorm:"column:id;primaryKey"`
	SubscriptionID uint      `json:"subscription_id" gorm:"column:subscription_id"`
	EventType      string    `json:"event_type" gorm:"column:event_type"`
	ScoreBefore    int       `json:"score_before" gorm:"column:score_before"`
	ScoreAfter     int       `json:"score_after" gorm:"column:score_after"`
	StateBefore    string    `json:"state_before" gorm:"column:state_before"`
	StateAfter     string    `json:"state_after" gorm:"column:state_after"`
	MetricsJSON    string    `json:"-" gorm:"column:metrics_json"`
	Reason         string    `json:"reason" gorm:"column:reason"`
	OccurredAt     time.Time `json:"occurred_at" gorm:"column:occurred_at"`
	CreatedAt      time.Time `json:"created_at" gorm:"column:created_at"`
}

func (subscriptionFairUseEvent) TableName() string { return "subscription_fair_use_events" }

type fairUseEventView struct {
	ID             uint64          `json:"id"`
	SubscriptionID uint            `json:"subscription_id"`
	EventType      string          `json:"event_type"`
	ScoreBefore    int             `json:"score_before"`
	ScoreAfter     int             `json:"score_after"`
	StateBefore    string          `json:"state_before"`
	StateAfter     string          `json:"state_after"`
	Metrics        json.RawMessage `json:"metrics"`
	Reason         string          `json:"reason"`
	OccurredAt     time.Time       `json:"occurred_at"`
}

type fairUsePolicyInput struct {
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

type fairUseEvaluationResult struct {
	SubscriptionID uint                     `json:"subscription_id"`
	Evaluated      bool                     `json:"evaluated"`
	Skipped        bool                     `json:"skipped"`
	Reason         string                   `json:"reason"`
	Policy         fairUsePolicy            `json:"policy"`
	PolicySource   fairUsePolicySource      `json:"policy_source"`
	State          subscriptionFairUseState `json:"state"`
	Metrics        *fairUseTelemetryMetrics `json:"metrics,omitempty"`
}

type fairUseEvaluationRuntime struct {
	cancel context.CancelFunc
	done   chan struct{}
}

var fairUseEvaluationRegistry sync.Map

func defaultFairUsePolicy(scopeType string, scopeID uint) fairUsePolicy {
	return fairUsePolicy{
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

func validateFairUsePolicy(input fairUsePolicyInput) error {
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
		return validationError("invalid Fair Use policy", fields)
	}
	return nil
}

func fairUsePolicyFromInput(scopeType string, scopeID uint, input fairUsePolicyInput, revision uint64, now time.Time) fairUsePolicy {
	return fairUsePolicy{
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

func parseFairUseResourceID(path, prefix, suffix string) (uint, error) {
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return 0, errors.New("invalid Fair Use path")
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	if raw == "" || strings.Contains(raw, "/") {
		return 0, errors.New("invalid resource id")
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		return 0, errors.New("invalid resource id")
	}
	id := uint(value)
	if uint64(id) != value {
		return 0, errors.New("resource id is out of range")
	}
	return id, nil
}

func parseFairUseResourceSubscriptionID(path, suffix string) (uint, error) {
	return parseFairUseResourceID(path, "/api/v1/admin/subscriptions/", suffix)
}

func parseFairUseResourcePlanID(path, suffix string) (uint, error) {
	return parseFairUseResourceID(path, "/api/v1/admin/plans/", suffix)
}

func (h *handlers) fairUseSubscriptionExists(subscriptionID uint) (bool, error) {
	var count int64
	if err := h.db.Model(&model.Subscription{}).Where("id = ?", subscriptionID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (h *handlers) fairUsePlanExists(planID uint) (bool, error) {
	var count int64
	if err := h.db.Model(&model.Plan{}).Where("id = ?", planID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func fairUsePolicySourceOf(policy fairUsePolicy) fairUsePolicySource {
	return fairUsePolicySource{ScopeType: policy.ScopeType, ScopeID: policy.ScopeID}
}

func chooseFairUsePolicy(subscriptionOverride, planOverride, platformOverride *fairUsePolicy) (fairUsePolicy, fairUsePolicySource) {
	if subscriptionOverride != nil {
		return *subscriptionOverride, fairUsePolicySourceOf(*subscriptionOverride)
	}
	if planOverride != nil {
		return *planOverride, fairUsePolicySourceOf(*planOverride)
	}
	if platformOverride != nil {
		return *platformOverride, fairUsePolicySourceOf(*platformOverride)
	}
	fallback := defaultFairUsePolicy(fairUsePolicyScopePlatform, 0)
	return fallback, fairUsePolicySourceOf(fallback)
}

func (h *handlers) loadFairUsePolicy(scopeType string, scopeID uint) (*fairUsePolicy, error) {
	var policy fairUsePolicy
	err := h.db.Where("scope_type = ? AND scope_id = ?", scopeType, scopeID).First(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &policy, nil
}

func (h *handlers) effectiveFairUsePolicy(subscriptionID uint) (fairUsePolicy, fairUsePolicySource, error) {
	var subscription model.Subscription
	if err := h.db.Select("id", "plan_id").First(&subscription, subscriptionID).Error; err != nil {
		return fairUsePolicy{}, fairUsePolicySource{}, err
	}
	subscriptionOverride, err := h.loadFairUsePolicy(fairUsePolicyScopeSubscription, subscription.ID)
	if err != nil {
		return fairUsePolicy{}, fairUsePolicySource{}, err
	}
	planOverride, err := h.loadFairUsePolicy(fairUsePolicyScopePlan, subscription.PlanID)
	if err != nil {
		return fairUsePolicy{}, fairUsePolicySource{}, err
	}
	platformOverride, err := h.loadFairUsePolicy(fairUsePolicyScopePlatform, 0)
	if err != nil {
		return fairUsePolicy{}, fairUsePolicySource{}, err
	}
	policy, source := chooseFairUsePolicy(subscriptionOverride, planOverride, platformOverride)
	return policy, source, nil
}

func (h *handlers) fairUsePolicyResolution(scopeType string, scopeID uint) (fairUsePolicyResolution, error) {
	override, err := h.loadFairUsePolicy(scopeType, scopeID)
	if err != nil {
		return fairUsePolicyResolution{}, err
	}
	resolution := fairUsePolicyResolution{Configured: override != nil, Override: override}
	switch scopeType {
	case fairUsePolicyScopePlatform:
		if override == nil {
			resolution.Effective = defaultFairUsePolicy(fairUsePolicyScopePlatform, 0)
		} else {
			resolution.Effective = *override
		}
		resolution.Source = fairUsePolicySourceOf(resolution.Effective)
	case fairUsePolicyScopePlan:
		platform, err := h.loadFairUsePolicy(fairUsePolicyScopePlatform, 0)
		if err != nil {
			return fairUsePolicyResolution{}, err
		}
		resolution.Effective, resolution.Source = chooseFairUsePolicy(nil, override, platform)
	case fairUsePolicyScopeSubscription:
		var subscription model.Subscription
		if err := h.db.Select("id", "plan_id").First(&subscription, scopeID).Error; err != nil {
			return fairUsePolicyResolution{}, err
		}
		plan, err := h.loadFairUsePolicy(fairUsePolicyScopePlan, subscription.PlanID)
		if err != nil {
			return fairUsePolicyResolution{}, err
		}
		platform, err := h.loadFairUsePolicy(fairUsePolicyScopePlatform, 0)
		if err != nil {
			return fairUsePolicyResolution{}, err
		}
		resolution.Effective, resolution.Source = chooseFairUsePolicy(override, plan, platform)
	default:
		return fairUsePolicyResolution{}, errors.New("invalid Fair Use policy scope")
	}
	return resolution, nil
}

var errFairUsePolicyRevisionConflict = errors.New("Fair Use policy revision conflict")

func (h *handlers) saveFairUsePolicy(scopeType string, scopeID uint, input fairUsePolicyInput, now time.Time) (fairUsePolicy, error) {
	if err := validateFairUsePolicy(input); err != nil {
		return fairUsePolicy{}, err
	}
	var saved fairUsePolicy
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var current fairUsePolicy
		lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("scope_type = ? AND scope_id = ?", scopeType, scopeID).First(&current)
		switch {
		case errors.Is(lookup.Error, gorm.ErrRecordNotFound):
			if input.ExpectedRevision != 0 {
				return errFairUsePolicyRevisionConflict
			}
			saved = fairUsePolicyFromInput(scopeType, scopeID, input, 1, now)
			saved.CreatedAt = now
			return tx.Create(&saved).Error
		case lookup.Error != nil:
			return lookup.Error
		case input.ExpectedRevision == 0 || input.ExpectedRevision != current.Revision:
			return errFairUsePolicyRevisionConflict
		default:
			saved = fairUsePolicyFromInput(scopeType, scopeID, input, current.Revision+1, now)
			saved.CreatedAt = current.CreatedAt
			updates := map[string]interface{}{
				"enabled":                         saved.Enabled,
				"evaluation_interval_seconds":     saved.EvaluationIntervalSeconds,
				"connection_start_threshold":      saved.ConnectionStartThreshold,
				"connection_start_window_seconds": saved.ConnectionStartWindowSeconds,
				"connection_start_penalty":        saved.ConnectionStartPenalty,
				"working_node_threshold":          saved.WorkingNodeThreshold,
				"working_node_window_seconds":     saved.WorkingNodeWindowSeconds,
				"working_node_penalty":            saved.WorkingNodePenalty,
				"score_max":                       saved.ScoreMax,
				"recovery_per_interval":           saved.RecoveryPerInterval,
				"warning_score":                   saved.WarningScore,
				"violation_score":                 saved.ViolationScore,
				"enforcement_mode":                saved.EnforcementMode,
				"restriction_duration_seconds":    saved.RestrictionDurationSeconds,
				"revision":                        saved.Revision,
				"updated_at":                      saved.UpdatedAt,
			}
			result := tx.Model(&fairUsePolicy{}).Where("scope_type = ? AND scope_id = ? AND revision = ?", scopeType, scopeID, current.Revision).Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errFairUsePolicyRevisionConflict
			}
			return nil
		}
	})
	return saved, err
}

func decodeFairUsePolicyInput(r *http.Request) (fairUsePolicyInput, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, nodeReportMaxBodyBytes+1))
	if err != nil || len(body) == 0 || len(body) > nodeReportMaxBodyBytes {
		return fairUsePolicyInput{}, errors.New("invalid Fair Use policy body")
	}
	var input fairUsePolicyInput
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return fairUsePolicyInput{}, errors.New("invalid Fair Use policy JSON")
	}
	return input, nil
}

func writeFairUsePolicyError(w http.ResponseWriter, err error) {
	var validation *requestValidationError
	switch {
	case errors.As(err, &validation):
		BadRequestFields(w, validation.message, validation.fields)
	case errors.Is(err, errFairUsePolicyRevisionConflict):
		writeJSON(w, http.StatusConflict, "Fair Use policy revision conflict", nil)
	default:
		ServerError(w, err)
	}
}

func (h *handlers) AdminPlatformFairUsePolicyHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		resolution, err := h.fairUsePolicyResolution(fairUsePolicyScopePlatform, 0)
		if err != nil {
			ServerError(w, err)
			return
		}
		OK(w, resolution)
	case http.MethodPut:
		input, err := decodeFairUsePolicyInput(r)
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
		if _, err := h.saveFairUsePolicy(fairUsePolicyScopePlatform, 0, input, time.Now().UTC()); err != nil {
			writeFairUsePolicyError(w, err)
			return
		}
		resolution, err := h.fairUsePolicyResolution(fairUsePolicyScopePlatform, 0)
		if err != nil {
			ServerError(w, err)
			return
		}
		OK(w, resolution)
	default:
		BadRequest(w, "unsupported Fair Use policy method")
	}
}

func (h *handlers) AdminPlanFairUsePolicyHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	planID, err := parseFairUseResourcePlanID(r.URL.Path, "/fair-use/policy")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	exists, err := h.fairUsePlanExists(planID)
	if err != nil {
		ServerError(w, err)
		return
	}
	if !exists {
		NotFound(w)
		return
	}
	h.handleScopedFairUsePolicy(w, r, fairUsePolicyScopePlan, planID)
}

func (h *handlers) AdminSubscriptionFairUsePolicyHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	subscriptionID, err := parseFairUseResourceSubscriptionID(r.URL.Path, "/fair-use/policy")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	exists, err := h.fairUseSubscriptionExists(subscriptionID)
	if err != nil {
		ServerError(w, err)
		return
	}
	if !exists {
		NotFound(w)
		return
	}
	h.handleScopedFairUsePolicy(w, r, fairUsePolicyScopeSubscription, subscriptionID)
}

func (h *handlers) handleScopedFairUsePolicy(w http.ResponseWriter, r *http.Request, scopeType string, scopeID uint) {
	switch r.Method {
	case http.MethodGet:
		resolution, err := h.fairUsePolicyResolution(scopeType, scopeID)
		if err != nil {
			ServerError(w, err)
			return
		}
		OK(w, resolution)
	case http.MethodPut:
		input, err := decodeFairUsePolicyInput(r)
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
		if _, err := h.saveFairUsePolicy(scopeType, scopeID, input, time.Now().UTC()); err != nil {
			writeFairUsePolicyError(w, err)
			return
		}
		resolution, err := h.fairUsePolicyResolution(scopeType, scopeID)
		if err != nil {
			ServerError(w, err)
			return
		}
		OK(w, resolution)
	case http.MethodDelete:
		if err := h.db.Where("scope_type = ? AND scope_id = ?", scopeType, scopeID).Delete(&fairUsePolicy{}).Error; err != nil {
			ServerError(w, err)
			return
		}
		resolution, err := h.fairUsePolicyResolution(scopeType, scopeID)
		if err != nil {
			ServerError(w, err)
			return
		}
		OK(w, resolution)
	default:
		BadRequest(w, "unsupported Fair Use policy method")
	}
}

func (h *handlers) AdminSubscriptionFairUseStateHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	subscriptionID, err := parseFairUseResourceSubscriptionID(r.URL.Path, "/fair-use/state")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	exists, err := h.fairUseSubscriptionExists(subscriptionID)
	if err != nil {
		ServerError(w, err)
		return
	}
	if !exists {
		NotFound(w)
		return
	}
	var state subscriptionFairUseState
	if err := h.db.Where("subscription_id = ?", subscriptionID).First(&state).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		OK(w, subscriptionFairUseState{SubscriptionID: subscriptionID, State: fairUseStateNormal, TelemetryCompleteness: "unknown"})
		return
	} else if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, state)
}

func (h *handlers) AdminSubscriptionFairUseEventsHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	subscriptionID, err := parseFairUseResourceSubscriptionID(r.URL.Path, "/fair-use/events")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	exists, err := h.fairUseSubscriptionExists(subscriptionID)
	if err != nil {
		ServerError(w, err)
		return
	}
	if !exists {
		NotFound(w)
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			BadRequest(w, "limit must be between 1 and 200")
			return
		}
		limit = parsed
	}
	var rows []subscriptionFairUseEvent
	if err := h.db.Where("subscription_id = ?", subscriptionID).Order("occurred_at desc, id desc").Limit(limit).Find(&rows).Error; err != nil {
		ServerError(w, err)
		return
	}
	views := make([]fairUseEventView, 0, len(rows))
	for _, row := range rows {
		metrics := json.RawMessage(row.MetricsJSON)
		if !json.Valid(metrics) {
			metrics = json.RawMessage(`{}`)
		}
		views = append(views, fairUseEventView{
			ID:             row.ID,
			SubscriptionID: row.SubscriptionID,
			EventType:      row.EventType,
			ScoreBefore:    row.ScoreBefore,
			ScoreAfter:     row.ScoreAfter,
			StateBefore:    row.StateBefore,
			StateAfter:     row.StateAfter,
			Metrics:        metrics,
			Reason:         row.Reason,
			OccurredAt:     row.OccurredAt.UTC(),
		})
	}
	OK(w, views)
}

func (h *handlers) AdminSubscriptionFairUseEvaluateHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	subscriptionID, err := parseFairUseResourceSubscriptionID(r.URL.Path, "/fair-use/evaluate")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	result, err := h.evaluateFairUseSubscription(subscriptionID, time.Now().UTC())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, result)
}

func fairUseRiskState(score int, policy fairUsePolicy) string {
	switch {
	case score >= policy.ViolationScore:
		return fairUseStateViolated
	case score >= policy.WarningScore:
		return fairUseStateSuspected
	default:
		return fairUseStateNormal
	}
}

func fairUseScoreTransition(policy fairUsePolicy, score int, connectionStarts, workingNodes int64) (int, string, string) {
	penalty := 0
	reasons := make([]string, 0, 2)
	if connectionStarts > int64(policy.ConnectionStartThreshold) {
		penalty += policy.ConnectionStartPenalty
		reasons = append(reasons, fmt.Sprintf("connection_starts=%d>%d:+%d", connectionStarts, policy.ConnectionStartThreshold, policy.ConnectionStartPenalty))
	}
	if workingNodes > int64(policy.WorkingNodeThreshold) {
		penalty += policy.WorkingNodePenalty
		reasons = append(reasons, fmt.Sprintf("working_nodes=%d>%d:+%d", workingNodes, policy.WorkingNodeThreshold, policy.WorkingNodePenalty))
	}
	if penalty == 0 {
		next := score - policy.RecoveryPerInterval
		if next < 0 {
			next = 0
		}
		return next, fairUseRiskState(next, policy), fmt.Sprintf("normal interval: recovery -%d", policy.RecoveryPerInterval)
	}
	next := score + penalty
	if next > policy.ScoreMax {
		next = policy.ScoreMax
	}
	return next, fairUseRiskState(next, policy), strings.Join(reasons, "; ")
}

func (h *handlers) evaluateFairUseSubscription(subscriptionID uint, now time.Time) (fairUseEvaluationResult, error) {
	policy, source, err := h.effectiveFairUsePolicy(subscriptionID)
	if err != nil {
		return fairUseEvaluationResult{}, err
	}
	result := fairUseEvaluationResult{SubscriptionID: subscriptionID, Policy: policy, PolicySource: source}
	if !policy.Enabled {
		result.Skipped = true
		result.Reason = "policy_disabled"
		return result, nil
	}
	due, state, err := h.fairUseEvaluationDue(subscriptionID, policy.EvaluationIntervalSeconds, now)
	if err != nil {
		return result, err
	}
	if !due {
		result.State = state
		result.Skipped = true
		result.Reason = "evaluation_interval_not_due"
		return result, nil
	}

	metrics, err := h.loadFairUseTelemetryMetrics(subscriptionID, policy.ConnectionStartWindowSeconds, policy.WorkingNodeWindowSeconds, now)
	if err != nil {
		return result, err
	}
	result.Metrics = &metrics
	metricsJSON, err := json.Marshal(metrics)
	if err != nil {
		return result, err
	}

	err = h.db.Transaction(func(tx *gorm.DB) error {
		var state subscriptionFairUseState
		lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("subscription_id = ?", subscriptionID).First(&state)
		if errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
			state = subscriptionFairUseState{
				SubscriptionID:        subscriptionID,
				State:                 fairUseStateNormal,
				TelemetryCompleteness: "unknown",
				CreatedAt:             now,
				UpdatedAt:             now,
			}
			if err := tx.Create(&state).Error; err != nil {
				return err
			}
		} else if lookup.Error != nil {
			return lookup.Error
		}
		if state.LastEvaluatedAt != nil && state.LastEvaluatedAt.Add(time.Duration(policy.EvaluationIntervalSeconds)*time.Second).After(now) {
			result.State = state
			result.Skipped = true
			result.Reason = "evaluation_interval_not_due"
			return nil
		}

		hadPreviousEvaluation := state.LastEvaluatedAt != nil
		beforeScore := state.Score
		beforeState := state.State
		beforeCompleteness := state.TelemetryCompleteness
		state.CurrentActiveFlows = metrics.CurrentActiveFlows
		state.ConnectionStarts = int(metrics.ReceivedConnectionStarts.Count)
		state.WorkingNodes = int(metrics.ReceivedWorkingNodes.Count)
		state.TelemetryCompleteness = metrics.TelemetryCompleteness
		evaluatedAt := now
		state.LastEvaluatedAt = &evaluatedAt
		state.UpdatedAt = now

		eventType := ""
		reason := ""
		if metrics.TelemetryCompleteness != "complete" {
			reason = "telemetry_" + metrics.TelemetryCompleteness + ": " + metrics.Coverage.Reason
			result.Skipped = true
			if !hadPreviousEvaluation || beforeCompleteness != metrics.TelemetryCompleteness {
				eventType = "coverage_changed"
			}
		} else {
			nextScore, nextState, scoreReason := fairUseScoreTransition(policy, state.Score, metrics.ReceivedConnectionStarts.Count, metrics.ReceivedWorkingNodes.Count)
			state.Score = nextScore
			state.State = nextState
			completeAt := now
			state.LastCompleteAt = &completeAt
			reason = scoreReason
			result.Evaluated = true
			switch {
			case beforeState != nextState:
				eventType = "state_changed"
			case nextScore > beforeScore:
				eventType = "risk_increased"
			case nextScore < beforeScore:
				eventType = "risk_recovered"
			case beforeCompleteness != "complete":
				eventType = "coverage_restored"
			}
		}
		if err := tx.Model(&subscriptionFairUseState{}).Where("subscription_id = ?", subscriptionID).Updates(map[string]interface{}{
			"score":                  state.Score,
			"state":                  state.State,
			"current_active_flows":   state.CurrentActiveFlows,
			"connection_starts":      state.ConnectionStarts,
			"working_nodes":          state.WorkingNodes,
			"telemetry_completeness": state.TelemetryCompleteness,
			"last_evaluated_at":      state.LastEvaluatedAt,
			"last_complete_at":       state.LastCompleteAt,
			"updated_at":             state.UpdatedAt,
		}).Error; err != nil {
			return err
		}
		if eventType != "" {
			event := subscriptionFairUseEvent{
				SubscriptionID: subscriptionID,
				EventType:      eventType,
				ScoreBefore:    beforeScore,
				ScoreAfter:     state.Score,
				StateBefore:    beforeState,
				StateAfter:     state.State,
				MetricsJSON:    string(metricsJSON),
				Reason:         reason,
				OccurredAt:     now,
				CreatedAt:      now,
			}
			if err := tx.Create(&event).Error; err != nil {
				return err
			}
		}
		result.State = state
		result.Reason = reason
		return nil
	})
	return result, err
}

func (h *handlers) fairUseEvaluationDue(subscriptionID uint, intervalSeconds int, now time.Time) (bool, subscriptionFairUseState, error) {
	var state subscriptionFairUseState
	err := h.db.Where("subscription_id = ?", subscriptionID).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, subscriptionFairUseState{}, nil
	}
	if err != nil {
		return false, subscriptionFairUseState{}, err
	}
	if state.LastEvaluatedAt == nil {
		return true, state, nil
	}
	return fairUseEvaluationIsDue(state.LastEvaluatedAt, intervalSeconds, now), state, nil
}

func fairUseEvaluationIsDue(lastEvaluatedAt *time.Time, intervalSeconds int, now time.Time) bool {
	if lastEvaluatedAt == nil {
		return true
	}
	return !lastEvaluatedAt.Add(time.Duration(intervalSeconds) * time.Second).After(now)
}

func (h *handlers) StartFairUseEvaluationWorker() {
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &fairUseEvaluationRuntime{cancel: cancel, done: make(chan struct{})}
	if _, loaded := fairUseEvaluationRegistry.LoadOrStore(h, runtime); loaded {
		cancel()
		return
	}
	go func() {
		defer close(runtime.done)
		h.runFairUseEvaluationCycle(time.Now().UTC())
		ticker := time.NewTicker(fairUseEvaluationWorkerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				h.runFairUseEvaluationCycle(now.UTC())
			}
		}
	}()
}

func (h *handlers) CloseFairUseEvaluationWorker() {
	value, ok := fairUseEvaluationRegistry.LoadAndDelete(h)
	if !ok {
		return
	}
	runtime := value.(*fairUseEvaluationRuntime)
	runtime.cancel()
	<-runtime.done
}

func (h *handlers) fairUseEvaluationCandidateIDs(now time.Time) ([]uint, error) {
	var ids []uint
	err := h.db.Table("subscriptions").
		Select("subscriptions.id").
		Joins("LEFT JOIN fair_use_policies AS subscription_policy ON subscription_policy.scope_type = ? AND subscription_policy.scope_id = subscriptions.id", fairUsePolicyScopeSubscription).
		Joins("LEFT JOIN fair_use_policies AS plan_policy ON plan_policy.scope_type = ? AND plan_policy.scope_id = subscriptions.plan_id", fairUsePolicyScopePlan).
		Joins("LEFT JOIN fair_use_policies AS platform_policy ON platform_policy.scope_type = ? AND platform_policy.scope_id = ?", fairUsePolicyScopePlatform, 0).
		Joins("LEFT JOIN subscription_fair_use_states AS evaluation_state ON evaluation_state.subscription_id = subscriptions.id").
		Where("subscriptions.status = ? AND subscriptions.end_at > ? AND subscriptions.flow_used < subscriptions.flow_total", subStatusActive, now).
		Where("COALESCE(subscription_policy.enabled, plan_policy.enabled, platform_policy.enabled, ?) = ?", false, true).
		Where(`(evaluation_state.last_evaluated_at IS NULL OR TIMESTAMPADD(SECOND,
			COALESCE(subscription_policy.evaluation_interval_seconds, plan_policy.evaluation_interval_seconds,
				platform_policy.evaluation_interval_seconds, ?), evaluation_state.last_evaluated_at) <= ?)`, fairUseDefaultEvaluationInterval, now).
		Order("evaluation_state.last_evaluated_at IS NOT NULL asc, evaluation_state.last_evaluated_at asc, subscriptions.id asc").
		Limit(fairUseEvaluationBatchSize).
		Pluck("subscriptions.id", &ids).Error
	return ids, err
}

func (h *handlers) runFairUseEvaluationCycle(now time.Time) {
	subscriptionIDs, err := h.fairUseEvaluationCandidateIDs(now)
	if err != nil {
		log.Printf("fair use evaluation policy scan failed: %v", err)
		return
	}
	for _, subscriptionID := range subscriptionIDs {
		result, err := h.evaluateFairUseSubscription(subscriptionID, now)
		if err != nil {
			log.Printf("fair use evaluation failed: subscription_id=%d error=%v", subscriptionID, err)
			continue
		}
		if result.Evaluated && result.State.State != fairUseStateNormal {
			log.Printf("fair use evaluation state: subscription_id=%d state=%s score=%d source=%s:%d reason=%s", subscriptionID, result.State.State, result.State.Score, result.PolicySource.ScopeType, result.PolicySource.ScopeID, result.Reason)
		}
	}
}
