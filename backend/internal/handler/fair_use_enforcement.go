package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	fairUseRestrictionSourceAutomatic = "automatic"
	fairUseRestrictionSourceManual    = "manual"
	fairUseRestrictionWorkerInterval  = 15 * time.Second
)

type subscriptionFairUseRestriction struct {
	SubscriptionID         uint       `json:"subscription_id" gorm:"column:subscription_id;primaryKey"`
	Active                 bool       `json:"active" gorm:"column:active"`
	Source                 string     `json:"source" gorm:"column:source"`
	PolicyScopeType        string     `json:"policy_scope_type" gorm:"column:policy_scope_type"`
	PolicyScopeID          uint       `json:"policy_scope_id" gorm:"column:policy_scope_id"`
	PolicyRevision         uint64     `json:"policy_revision" gorm:"column:policy_revision"`
	Score                  int        `json:"score" gorm:"column:score"`
	Reason                 string     `json:"reason" gorm:"column:reason"`
	StartedAt              *time.Time `json:"started_at,omitempty" gorm:"column:started_at"`
	RestrictedUntil        *time.Time `json:"restricted_until,omitempty" gorm:"column:restricted_until"`
	ReleasedAt             *time.Time `json:"released_at,omitempty" gorm:"column:released_at"`
	ReleaseReason          string     `json:"release_reason" gorm:"column:release_reason"`
	HoldUntil              *time.Time `json:"hold_until,omitempty" gorm:"column:hold_until"`
	LastSourceEvaluationAt *time.Time `json:"last_source_evaluation_at,omitempty" gorm:"column:last_source_evaluation_at"`
	Revision               uint64     `json:"revision" gorm:"column:revision"`
	CreatedAt              time.Time  `json:"created_at" gorm:"column:created_at"`
	UpdatedAt              time.Time  `json:"updated_at" gorm:"column:updated_at"`
}

func (subscriptionFairUseRestriction) TableName() string {
	return "subscription_fair_use_restrictions"
}

type fairUseRestrictionView struct {
	Restriction subscriptionFairUseRestriction `json:"restriction"`
	Effective   bool                           `json:"effective"`
	Enforceable bool                           `json:"enforceable"`
}

type fairUseManualRestrictionInput struct {
	DurationSeconds int    `json:"duration_seconds"`
	Reason          string `json:"reason"`
}

type fairUseRestrictionRuntime struct {
	cancel context.CancelFunc
	done   chan struct{}
}

var fairUseRestrictionRegistry sync.Map

func fairUseRestrictionActive(row subscriptionFairUseRestriction, now time.Time) bool {
	return row.Active && row.RestrictedUntil != nil && row.RestrictedUntil.After(now.UTC())
}

func fairUseAutomaticRestrictionEligible(policy fairUsePolicy, state subscriptionFairUseState, row *subscriptionFairUseRestriction, now time.Time) bool {
	if !policy.Enabled || policy.EnforcementMode != fairUseEnforcementRestrict || state.State != fairUseStateViolated || state.TelemetryCompleteness != "complete" || state.LastCompleteAt == nil || state.LastEvaluatedAt == nil {
		return false
	}
	if !state.LastCompleteAt.Equal(*state.LastEvaluatedAt) {
		return false
	}
	if now.Sub(state.LastCompleteAt.UTC()) > 2*time.Duration(policy.EvaluationIntervalSeconds)*time.Second {
		return false
	}
	if row == nil {
		return true
	}
	if fairUseRestrictionActive(*row, now) {
		return false
	}
	if row.HoldUntil != nil && row.HoldUntil.After(now.UTC()) {
		return false
	}
	if row.LastSourceEvaluationAt != nil && !state.LastCompleteAt.After(*row.LastSourceEvaluationAt) {
		return false
	}
	return true
}

func (h *handlers) isFairUseRestrictedSubscription(subscriptionID uint, now time.Time) (bool, error) {
	var count int64
	if err := h.db.Model(&subscriptionFairUseRestriction{}).
		Where("subscription_id = ? AND active = ? AND restricted_until > ?", subscriptionID, true, now.UTC()).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (h *handlers) fairUseRestrictionEnforceable(subscriptionID uint) (bool, error) {
	var subscription model.Subscription
	if err := h.db.Select("id", "node_group_id").First(&subscription, subscriptionID).Error; err != nil {
		return false, err
	}
	var endpoints []model.ProtocolEndpoint
	if err := h.db.Model(&model.ProtocolEndpoint{}).
		Joins("JOIN node_group_endpoints ON node_group_endpoints.protocol_endpoint_id = protocol_endpoints.id").
		Where("node_group_endpoints.node_group_id = ? AND protocol_endpoints.is_active = ?", subscription.NodeGroupID, true).
		Order("protocol_endpoints.id asc").Find(&endpoints).Error; err != nil {
		return false, err
	}
	if len(endpoints) == 0 {
		return false, nil
	}
	for _, endpoint := range endpoints {
		if !h.endpointDeliversSubscriptionCredential(endpoint) {
			return false, nil
		}
	}
	return true, nil
}

func (h *handlers) appendFairUseRestrictionEvent(tx *gorm.DB, row subscriptionFairUseRestriction, eventType, reason string, now time.Time) error {
	payload, _ := json.Marshal(map[string]interface{}{
		"source": row.Source, "policy_scope_type": row.PolicyScopeType, "policy_scope_id": row.PolicyScopeID,
		"policy_revision": row.PolicyRevision, "restricted_until": row.RestrictedUntil,
	})
	return tx.Create(&subscriptionFairUseEvent{
		SubscriptionID: row.SubscriptionID, EventType: eventType,
		ScoreBefore: row.Score, ScoreAfter: row.Score, StateBefore: fairUseStateViolated, StateAfter: fairUseStateViolated,
		MetricsJSON: string(payload), Reason: reason, OccurredAt: now, CreatedAt: now,
	}).Error
}

func (h *handlers) applyFairUseRestriction(subscriptionID uint, source string, policy fairUsePolicy, policySource fairUsePolicySource, state subscriptionFairUseState, duration time.Duration, reason string, now time.Time) (subscriptionFairUseRestriction, bool, error) {
	if duration < time.Duration(fairUseMinRestrictionDurationSeconds)*time.Second || duration > time.Duration(fairUseMaxRestrictionDurationSeconds)*time.Second {
		return subscriptionFairUseRestriction{}, false, validationError("invalid Fair Use restriction", map[string]string{"duration_seconds": "must be between 60 and 604800"})
	}
	enforceable, err := h.fairUseRestrictionEnforceable(subscriptionID)
	if err != nil {
		return subscriptionFairUseRestriction{}, false, err
	}
	if !enforceable {
		return subscriptionFairUseRestriction{}, false, errors.New("subscription cannot be restricted safely on the current runtime credential projection")
	}
	now = now.UTC()
	until := now.Add(duration)
	var saved subscriptionFairUseRestriction
	applied := false
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var current subscriptionFairUseRestriction
		lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("subscription_id = ?", subscriptionID).First(&current)
		if lookup.Error != nil && !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
			return lookup.Error
		}
		if lookup.Error == nil && fairUseRestrictionActive(current, now) {
			saved = current
			return nil
		}
		if source == fairUseRestrictionSourceAutomatic {
			if lookup.Error == nil && current.HoldUntil != nil && current.HoldUntil.After(now) {
				saved = current
				return nil
			}
			if state.LastCompleteAt == nil || (lookup.Error == nil && current.LastSourceEvaluationAt != nil && !state.LastCompleteAt.After(*current.LastSourceEvaluationAt)) {
				saved = current
				return nil
			}
		}
		revision := uint64(1)
		createdAt := now
		if lookup.Error == nil {
			revision = current.Revision + 1
			createdAt = current.CreatedAt
		}
		saved = subscriptionFairUseRestriction{
			SubscriptionID: subscriptionID, Active: true, Source: source,
			PolicyScopeType: policySource.ScopeType, PolicyScopeID: policySource.ScopeID, PolicyRevision: policy.Revision,
			Score: state.Score, Reason: strings.TrimSpace(reason), StartedAt: &now, RestrictedUntil: &until,
			Revision: revision, CreatedAt: createdAt, UpdatedAt: now,
		}
		if source == fairUseRestrictionSourceAutomatic {
			saved.LastSourceEvaluationAt = state.LastCompleteAt
		}
		if lookup.Error == nil {
			if err := tx.Model(&subscriptionFairUseRestriction{}).Where("subscription_id = ?", subscriptionID).Updates(map[string]interface{}{
				"active": true, "source": saved.Source, "policy_scope_type": saved.PolicyScopeType, "policy_scope_id": saved.PolicyScopeID,
				"policy_revision": saved.PolicyRevision, "score": saved.Score, "reason": saved.Reason, "started_at": saved.StartedAt,
				"restricted_until": saved.RestrictedUntil, "released_at": nil, "release_reason": "", "hold_until": nil,
				"last_source_evaluation_at": saved.LastSourceEvaluationAt, "revision": saved.Revision, "updated_at": now,
			}).Error; err != nil {
				return err
			}
		} else if err := tx.Create(&saved).Error; err != nil {
			return err
		}
		if err := h.appendFairUseRestrictionEvent(tx, saved, "restriction_applied", saved.Reason, now); err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err == nil && applied {
		h.scheduleSubscriptionConfigPublishes(subscriptionID, 0)
	}
	return saved, applied, err
}

func (h *handlers) releaseFairUseRestriction(subscriptionID uint, reason string, now time.Time) (subscriptionFairUseRestriction, bool, error) {
	now = now.UTC()
	var saved subscriptionFairUseRestriction
	released := false
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var current subscriptionFairUseRestriction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("subscription_id = ?", subscriptionID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		saved = current
		if !current.Active {
			return nil
		}
		holdSeconds := fairUseMinEvaluationInterval
		if policy, _, err := h.effectiveFairUsePolicy(subscriptionID); err == nil && policy.EvaluationIntervalSeconds > holdSeconds {
			holdSeconds = policy.EvaluationIntervalSeconds
		}
		holdUntil := now.Add(time.Duration(holdSeconds) * time.Second)
		saved.Active = false
		saved.ReleasedAt = &now
		saved.ReleaseReason = strings.TrimSpace(reason)
		saved.HoldUntil = &holdUntil
		saved.Revision++
		saved.UpdatedAt = now
		if err := tx.Model(&subscriptionFairUseRestriction{}).Where("subscription_id = ?", subscriptionID).Updates(map[string]interface{}{
			"active": false, "released_at": now, "release_reason": saved.ReleaseReason, "hold_until": holdUntil,
			"revision": saved.Revision, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		if err := h.appendFairUseRestrictionEvent(tx, saved, "restriction_released", saved.ReleaseReason, now); err != nil {
			return err
		}
		released = true
		return nil
	})
	if err == nil && released {
		h.scheduleSubscriptionConfigPublishes(subscriptionID, 0)
	}
	return saved, released, err
}

func (h *handlers) reconcileAutomaticFairUseRestriction(subscriptionID uint, now time.Time) error {
	policy, source, err := h.effectiveFairUsePolicy(subscriptionID)
	if err != nil {
		return err
	}
	var state subscriptionFairUseState
	if err := h.db.Where("subscription_id = ?", subscriptionID).First(&state).Error; err != nil {
		return err
	}
	var row subscriptionFairUseRestriction
	rowErr := h.db.Where("subscription_id = ?", subscriptionID).First(&row).Error
	if rowErr != nil && !errors.Is(rowErr, gorm.ErrRecordNotFound) {
		return rowErr
	}
	var existing *subscriptionFairUseRestriction
	if rowErr == nil {
		existing = &row
	}
	if !fairUseAutomaticRestrictionEligible(policy, state, existing, now.UTC()) {
		return nil
	}
	_, _, err = h.applyFairUseRestriction(subscriptionID, fairUseRestrictionSourceAutomatic, policy, source, state,
		time.Duration(policy.RestrictionDurationSeconds)*time.Second,
		fmt.Sprintf("Fair Use violated: score=%d", state.Score), now)
	return err
}

func (h *handlers) reconcileActiveFairUseRestrictions(now time.Time) {
	var rows []subscriptionFairUseRestriction
	if err := h.db.Where("active = ?", true).Find(&rows).Error; err != nil {
		log.Printf("fair use restriction scan failed: %v", err)
		return
	}
	for _, row := range rows {
		reason := ""
		if row.RestrictedUntil == nil || !row.RestrictedUntil.After(now.UTC()) {
			reason = "restriction expired"
		} else if row.Source == fairUseRestrictionSourceAutomatic {
			policy, _, err := h.effectiveFairUsePolicy(row.SubscriptionID)
			if err != nil {
				log.Printf("fair use restriction policy resolution failed: subscription_id=%d error=%v", row.SubscriptionID, err)
				continue
			}
			if !policy.Enabled || policy.EnforcementMode != fairUseEnforcementRestrict {
				reason = "automatic restriction no longer enabled by effective policy"
			}
		}
		if reason != "" {
			if _, _, err := h.releaseFairUseRestriction(row.SubscriptionID, reason, now); err != nil {
				log.Printf("fair use restriction release failed: subscription_id=%d error=%v", row.SubscriptionID, err)
			}
		}
	}
}

func (h *handlers) runFairUseRestrictionCycle(now time.Time) {
	h.reconcileActiveFairUseRestrictions(now)
	var subscriptionIDs []uint
	if err := h.db.Model(&subscriptionFairUseState{}).
		Where("state = ? AND telemetry_completeness = ?", fairUseStateViolated, "complete").
		Order("subscription_id asc").Pluck("subscription_id", &subscriptionIDs).Error; err != nil {
		log.Printf("fair use restriction candidate scan failed: %v", err)
		return
	}
	for _, subscriptionID := range subscriptionIDs {
		if err := h.reconcileAutomaticFairUseRestriction(subscriptionID, now); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("fair use automatic restriction failed: subscription_id=%d error=%v", subscriptionID, err)
		}
	}
}

func (h *handlers) StartFairUseRestrictionWorker() {
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &fairUseRestrictionRuntime{cancel: cancel, done: make(chan struct{})}
	if _, loaded := fairUseRestrictionRegistry.LoadOrStore(h, runtime); loaded {
		cancel()
		return
	}
	go func() {
		defer close(runtime.done)
		h.runFairUseRestrictionCycle(time.Now().UTC())
		ticker := time.NewTicker(fairUseRestrictionWorkerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				h.runFairUseRestrictionCycle(now.UTC())
			}
		}
	}()
}

func (h *handlers) CloseFairUseRestrictionWorker() {
	value, ok := fairUseRestrictionRegistry.LoadAndDelete(h)
	if !ok {
		return
	}
	runtime := value.(*fairUseRestrictionRuntime)
	runtime.cancel()
	<-runtime.done
}

func (h *handlers) AdminSubscriptionFairUseRestrictionHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	subscriptionID, err := parseFairUseResourceSubscriptionID(r.URL.Path, "/fair-use/restriction")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	if exists, err := h.fairUseSubscriptionExists(subscriptionID); err != nil {
		ServerError(w, err)
		return
	} else if !exists {
		NotFound(w)
		return
	}
	now := time.Now().UTC()
	switch r.Method {
	case http.MethodGet:
		var row subscriptionFairUseRestriction
		err := h.db.Where("subscription_id = ?", subscriptionID).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row = subscriptionFairUseRestriction{SubscriptionID: subscriptionID}
		} else if err != nil {
			ServerError(w, err)
			return
		}
		enforceable, err := h.fairUseRestrictionEnforceable(subscriptionID)
		if err != nil {
			ServerError(w, err)
			return
		}
		OK(w, fairUseRestrictionView{Restriction: row, Effective: fairUseRestrictionActive(row, now), Enforceable: enforceable})
	case http.MethodPost:
		var input fairUseManualRestrictionInput
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, nodeReportMaxBodyBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			BadRequest(w, "invalid Fair Use restriction JSON")
			return
		}
		policy, source, err := h.effectiveFairUsePolicy(subscriptionID)
		if err != nil {
			ServerError(w, err)
			return
		}
		if input.DurationSeconds == 0 {
			input.DurationSeconds = policy.RestrictionDurationSeconds
		}
		if strings.TrimSpace(input.Reason) == "" {
			BadRequestFields(w, "invalid Fair Use restriction", map[string]string{"reason": "reason is required"})
			return
		}
		var state subscriptionFairUseState
		_ = h.db.Where("subscription_id = ?", subscriptionID).First(&state).Error
		row, applied, err := h.applyFairUseRestriction(subscriptionID, fairUseRestrictionSourceManual, policy, source, state,
			time.Duration(input.DurationSeconds)*time.Second, input.Reason, now)
		if err != nil {
			var validation *requestValidationError
			if errors.As(err, &validation) {
				BadRequestFields(w, validation.message, validation.fields)
				return
			}
			writeJSON(w, http.StatusConflict, err.Error(), nil)
			return
		}
		if !applied && fairUseRestrictionActive(row, now) {
			writeJSON(w, http.StatusConflict, "subscription already has an active Fair Use restriction", row)
			return
		}
		OK(w, row)
	case http.MethodDelete:
		reason := strings.TrimSpace(r.URL.Query().Get("reason"))
		if reason == "" {
			reason = "manual release"
		}
		row, _, err := h.releaseFairUseRestriction(subscriptionID, reason, now)
		if err != nil {
			ServerError(w, err)
			return
		}
		OK(w, row)
	default:
		BadRequest(w, "unsupported Fair Use restriction method")
	}
}
