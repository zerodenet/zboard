package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	fairUseDefaultConnectionStartWindowSeconds = 60
	fairUseDefaultWorkingNodeWindowSeconds     = 300
	fairUseMinConnectionStartWindowSeconds     = 10
	fairUseMinWorkingNodeWindowSeconds         = 30
	fairUseMaxTelemetryWindowSeconds           = 3600
)

type subscriptionFlowStartEvent struct {
	ID                   uint64    `gorm:"column:id;primaryKey"`
	NodeID               uint      `gorm:"column:node_id"`
	CoreInstanceID       string    `gorm:"column:core_instance_id"`
	EventID              string    `gorm:"column:event_id"`
	Sequence             uint64    `gorm:"column:sequence"`
	PrincipalKey         string    `gorm:"column:principal_key"`
	UserID               uint      `gorm:"column:user_id"`
	SubscriptionID       uint      `gorm:"column:subscription_id"`
	ProtocolCredentialID uint      `gorm:"column:protocol_credential_id"`
	ProtocolEndpointID   uint      `gorm:"column:protocol_endpoint_id"`
	MappingState         string    `gorm:"column:mapping_state"`
	OccurredAt           time.Time `gorm:"column:occurred_at"`
	ReceivedAt           time.Time `gorm:"column:received_at"`
	CreatedAt            time.Time `gorm:"column:created_at"`
}

func (subscriptionFlowStartEvent) TableName() string { return "subscription_flow_start_events" }

type fairUseWindowMetric struct {
	WindowSeconds int   `json:"window_seconds"`
	Count         int64 `json:"count"`
}

type fairUseTelemetryMetrics struct {
	SubscriptionID           uint                   `json:"subscription_id"`
	UserID                   uint                   `json:"user_id"`
	SampledAt                time.Time              `json:"sampled_at"`
	CurrentActiveFlows       *uint64                `json:"current_active_flows"`
	ConnectionStarts         fairUseWindowMetric    `json:"connection_starts"`
	ReceivedConnectionStarts fairUseWindowMetric    `json:"received_connection_starts"`
	WorkingNodes             fairUseWindowMetric    `json:"working_nodes"`
	ReceivedWorkingNodes     fairUseWindowMetric    `json:"received_working_nodes"`
	LastActivityAt           *time.Time             `json:"last_activity_at"`
	LastReceivedAt           *time.Time             `json:"last_received_at"`
	TelemetryCompleteness    string                 `json:"telemetry_completeness"`
	EvaluationReady          bool                   `json:"evaluation_ready"`
	EnforcementReady         bool                   `json:"enforcement_ready"`
	EventTimeBasis           string                 `json:"event_time_basis"`
	Coverage                 fairUseCoverageSummary `json:"coverage"`
}

// ZeroEventFairUseTelemetryHandler decorates the existing observability path.
// Traffic settlement and Principal projections remain authoritative. Fair Use
// collection only runs after that path accepts the event, so rejected or
// unauthenticated events can never influence behavioural metrics. Fair Use is
// also fail-open: auxiliary telemetry persistence cannot turn an accepted Zero
// event into a failed delivery or interfere with traffic accounting.
func (h *handlers) ZeroEventFairUseTelemetryHandler(w http.ResponseWriter, r *http.Request) {
	state, err := h.prepareZeroEventRequest(r)
	if err != nil {
		writeZeroEventRequestError(w, err)
		return
	}
	r = withZeroEventState(r, state)
	recorded := httptest.NewRecorder()
	h.ZeroEventObservabilityHandler(recorded, r)
	if recorded.Code < http.StatusOK || recorded.Code >= http.StatusMultipleChoices {
		copyRecordedResponse(w, recorded)
		return
	}

	event := state.event
	if nodeID, ok := zeroEventSourceNodeID(event.SourceID); ok {
		// Buffered high-frequency events update coverage once per node and spool
		// batch in the projector transaction. Lifecycle facts remain synchronous.
		if !state.buffered {
			if err := h.observeFairUseEventCoverage(nodeID, event, state.receivedAt); err != nil {
				log.Printf("fair use coverage persistence failed: node_id=%d event_id=%q error=%v", nodeID, event.EventID, err)
			}
		}
		if event.EventType == "flow.started" {
			if err := h.persistSubscriptionFlowStartEvent(nodeID, event); err != nil {
				log.Printf("fair use flow-start telemetry persistence failed: node_id=%d event_id=%q error=%v", nodeID, event.EventID, err)
			}
		}
	}
	copyRecordedResponse(w, recorded)
}

func (h *handlers) persistSubscriptionFlowStartEvent(nodeID uint, event zeroEventEnvelope) error {
	if nodeID == 0 || event.EventType != "flow.started" {
		return nil
	}
	eventID := strings.TrimSpace(event.EventID)
	if eventID == "" {
		return nil
	}

	principalKey := flowStartedPrincipalKey(event)
	if isMieruMigrationPrincipal(principalKey) {
		return nil
	}
	now := time.Now().UTC()
	row := subscriptionFlowStartEvent{
		NodeID:         nodeID,
		CoreInstanceID: strings.TrimSpace(event.CoreInstanceID),
		EventID:        eventID,
		Sequence:       event.Sequence,
		PrincipalKey:   principalKey,
		MappingState:   "unmapped",
		OccurredAt:     zeroEventTime(event, now).UTC(),
		ReceivedAt:     now,
		CreatedAt:      now,
	}
	if principalKey != "" {
		credential, found, err := resolvePrincipalFlowCredential(h.db, nodeID, principalKey)
		if err != nil {
			return err
		}
		if found {
			row.UserID = credential.UserID
			row.SubscriptionID = credential.SubscriptionID
			row.ProtocolCredentialID = credential.ID
			row.ProtocolEndpointID = credential.ProtocolEndpointID
			if row.SubscriptionID > 0 {
				row.MappingState = "mapped"
			}
		}
	}
	return h.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

func flowStartedPrincipalKey(event zeroEventEnvelope) string {
	principal := strings.TrimSpace(event.PrincipalKey)
	var payload map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(event.Payload))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err == nil && payload != nil {
		if value := zeroEventPrincipalKey(event, payload); value != "" {
			principal = value
		}
	}
	return strings.TrimSpace(principal)
}

func parseFairUseWindow(raw string, fallback, minimum int) (int, error) {
	value := fallback
	if strings.TrimSpace(raw) != "" {
		parsed, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return 0, errors.New("window must be an integer number of seconds")
		}
		value = parsed
	}
	if value < minimum || value > fairUseMaxTelemetryWindowSeconds {
		return 0, errors.New("window is outside the supported telemetry range")
	}
	return value, nil
}

func parseFairUseSubscriptionID(path string) (uint, error) {
	return parseFairUseResourceSubscriptionID(path, "/fair-use/metrics")
}

func (h *handlers) loadFairUseTelemetryMetrics(subscriptionID uint, connectionWindow, workingNodeWindow int, now time.Time) (fairUseTelemetryMetrics, error) {
	var subscription model.Subscription
	if err := h.db.Select("id", "user_id").First(&subscription, subscriptionID).Error; err != nil {
		return fairUseTelemetryMetrics{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	metrics := fairUseTelemetryMetrics{
		SubscriptionID: subscription.ID,
		UserID:         subscription.UserID,
		SampledAt:      now,
		ConnectionStarts: fairUseWindowMetric{
			WindowSeconds: connectionWindow,
		},
		ReceivedConnectionStarts: fairUseWindowMetric{
			WindowSeconds: connectionWindow,
		},
		WorkingNodes: fairUseWindowMetric{
			WindowSeconds: workingNodeWindow,
		},
		ReceivedWorkingNodes: fairUseWindowMetric{
			WindowSeconds: workingNodeWindow,
		},
		TelemetryCompleteness: "unknown",
		EvaluationReady:       false,
		EnforcementReady:      false,
		EventTimeBasis:        "core_event_time_diagnostic_receive_time_evaluation",
	}

	connectionCutoff := now.Add(-time.Duration(connectionWindow) * time.Second)
	if err := h.db.Model(&subscriptionFlowStartEvent{}).
		Where("subscription_id = ? AND occurred_at >= ? AND occurred_at <= ?", subscription.ID, connectionCutoff, now).
		Count(&metrics.ConnectionStarts.Count).Error; err != nil {
		return metrics, err
	}
	if err := h.db.Model(&subscriptionFlowStartEvent{}).
		Where("subscription_id = ? AND received_at >= ? AND received_at <= ?", subscription.ID, connectionCutoff, now).
		Count(&metrics.ReceivedConnectionStarts.Count).Error; err != nil {
		return metrics, err
	}

	workingNodeCutoff := now.Add(-time.Duration(workingNodeWindow) * time.Second)
	if err := h.db.Model(&subscriptionFlowStartEvent{}).
		Where("subscription_id = ? AND occurred_at >= ? AND occurred_at <= ?", subscription.ID, workingNodeCutoff, now).
		Distinct("node_id").Count(&metrics.WorkingNodes.Count).Error; err != nil {
		return metrics, err
	}
	if err := h.db.Model(&subscriptionFlowStartEvent{}).
		Where("subscription_id = ? AND received_at >= ? AND received_at <= ?", subscription.ID, workingNodeCutoff, now).
		Distinct("node_id").Count(&metrics.ReceivedWorkingNodes.Count).Error; err != nil {
		return metrics, err
	}

	var current principalFlowScopeCurrent
	currentErr := h.db.Where("scope_type = ? AND scope_id = ?", principalFlowScopeSubscription, subscription.ID).First(&current).Error
	switch {
	case currentErr == nil:
		value := current.ActiveFlows
		metrics.CurrentActiveFlows = &value
	case !errors.Is(currentErr, gorm.ErrRecordNotFound):
		return metrics, currentErr
	}

	var latest subscriptionFlowStartEvent
	latestErr := h.db.Where("subscription_id = ?", subscription.ID).Order("occurred_at desc, id desc").First(&latest).Error
	switch {
	case latestErr == nil:
		occurred := latest.OccurredAt.UTC()
		received := latest.ReceivedAt.UTC()
		metrics.LastActivityAt = &occurred
		metrics.LastReceivedAt = &received
	case !errors.Is(latestErr, gorm.ErrRecordNotFound):
		return metrics, latestErr
	}

	coverageWindow := connectionWindow
	if workingNodeWindow > coverageWindow {
		coverageWindow = workingNodeWindow
	}
	coverage, err := h.fairUseCoverageForSubscription(subscription.ID, coverageWindow, now)
	if err != nil {
		return metrics, err
	}
	metrics.Coverage = coverage
	metrics.TelemetryCompleteness = coverage.State
	metrics.EvaluationReady = coverage.State == "complete"
	return metrics, nil
}

// AdminSubscriptionFairUseMetricsHandler exposes both diagnostic event-time
// and receive-time signals. Business evaluation uses receive-time windows only:
// Core clock skew or spool backlog must not look like subscriber behaviour.
func (h *handlers) AdminSubscriptionFairUseMetricsHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	subscriptionID, err := parseFairUseSubscriptionID(r.URL.Path)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	connectionWindow, err := parseFairUseWindow(
		r.URL.Query().Get("connection_window_seconds"),
		fairUseDefaultConnectionStartWindowSeconds,
		fairUseMinConnectionStartWindowSeconds,
	)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	workingNodeWindow, err := parseFairUseWindow(
		r.URL.Query().Get("working_node_window_seconds"),
		fairUseDefaultWorkingNodeWindowSeconds,
		fairUseMinWorkingNodeWindowSeconds,
	)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	metrics, err := h.loadFairUseTelemetryMetrics(subscriptionID, connectionWindow, workingNodeWindow, time.Now().UTC())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, metrics)
}
