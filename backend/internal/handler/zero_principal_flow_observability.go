package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	principalFlowScopeUser         = "user"
	principalFlowScopeSubscription = "subscription"
)

type principalFlowObservation struct {
	ID                      uint64    `gorm:"column:id;primaryKey"`
	NodeID                  uint      `gorm:"column:node_id"`
	CoreInstanceID          string    `gorm:"column:core_instance_id"`
	SessionRegistryRevision uint64    `gorm:"column:session_registry_revision"`
	EventID                 string    `gorm:"column:event_id"`
	Sequence                uint64    `gorm:"column:sequence"`
	PrincipalKey            string    `gorm:"column:principal_key"`
	UserID                  uint      `gorm:"column:user_id"`
	SubscriptionID          uint      `gorm:"column:subscription_id"`
	ProtocolCredentialID    uint      `gorm:"column:protocol_credential_id"`
	ProtocolEndpointID      uint      `gorm:"column:protocol_endpoint_id"`
	ActiveFlows             uint64    `gorm:"column:active_flows"`
	ObservedAt              time.Time `gorm:"column:observed_at"`
	CreatedAt               time.Time `gorm:"column:created_at"`
}

func (principalFlowObservation) TableName() string { return "principal_flow_observations" }

type principalFlowCurrent struct {
	NodeID                  uint      `gorm:"column:node_id;primaryKey"`
	PrincipalKey            string    `gorm:"column:principal_key;primaryKey"`
	CoreInstanceID          string    `gorm:"column:core_instance_id"`
	SessionRegistryRevision uint64    `gorm:"column:session_registry_revision"`
	UserID                  uint      `gorm:"column:user_id"`
	SubscriptionID          uint      `gorm:"column:subscription_id"`
	ProtocolCredentialID    uint      `gorm:"column:protocol_credential_id"`
	ProtocolEndpointID      uint      `gorm:"column:protocol_endpoint_id"`
	ActiveFlows             uint64    `gorm:"column:active_flows"`
	ObservedAt              time.Time `gorm:"column:observed_at"`
	UpdatedAt               time.Time `gorm:"column:updated_at"`
}

func (principalFlowCurrent) TableName() string { return "principal_flow_currents" }

type principalFlowNodeGeneration struct {
	NodeID         uint       `gorm:"column:node_id;primaryKey"`
	CoreInstanceID string     `gorm:"column:core_instance_id"`
	StartedAt      time.Time  `gorm:"column:started_at"`
	ClosedAt       *time.Time `gorm:"column:closed_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
}

func (principalFlowNodeGeneration) TableName() string { return "principal_flow_node_generations" }

type principalFlowScopeCurrent struct {
	ScopeType   string    `gorm:"column:scope_type;primaryKey"`
	ScopeID     uint      `gorm:"column:scope_id;primaryKey"`
	ActiveFlows uint64    `gorm:"column:active_flows"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

func (principalFlowScopeCurrent) TableName() string { return "principal_flow_scope_currents" }

type principalFlowScopeObservation struct {
	ID                      uint64    `gorm:"column:id;primaryKey"`
	ScopeType               string    `gorm:"column:scope_type"`
	ScopeID                 uint      `gorm:"column:scope_id"`
	ActiveFlows             uint64    `gorm:"column:active_flows"`
	NodeID                  uint      `gorm:"column:node_id"`
	CoreInstanceID          string    `gorm:"column:core_instance_id"`
	SessionRegistryRevision uint64    `gorm:"column:session_registry_revision"`
	EventID                 string    `gorm:"column:event_id"`
	Source                  string    `gorm:"column:source"`
	ObservedAt              time.Time `gorm:"column:observed_at"`
	CreatedAt               time.Time `gorm:"column:created_at"`
}

func (principalFlowScopeObservation) TableName() string {
	return "principal_flow_scope_observations"
}

type zeroPrincipalFlowObservation struct {
	PrincipalKey            string
	ActiveFlows             uint64
	SessionRegistryRevision uint64
	ObservedAt              time.Time
}

type principalFlowCredentialProjection struct {
	ID                 uint `gorm:"column:id"`
	UserID             uint `gorm:"column:user_id"`
	SubscriptionID     uint `gorm:"column:subscription_id"`
	ProtocolEndpointID uint `gorm:"column:protocol_endpoint_id"`
}

type principalFlowScopeTrendRow struct {
	Day         string `gorm:"column:day"`
	Peak        int64  `gorm:"column:peak"`
	SampleCount int64  `gorm:"column:sample_count"`
}

// ZeroEventObservabilityHandler decorates the existing accounting receiver.
// The existing handler remains authoritative for authentication, traffic
// settlement and connector activity. Only after that path accepts an event do
// we project additive Principal connection observations. Returning a 5xx after
// a projection failure is safe because the existing accounting path is
// idempotent and Core will retry its durable lifecycle fact.
func (h *handlers) ZeroEventObservabilityHandler(w http.ResponseWriter, r *http.Request) {
	body, readErr := io.ReadAll(io.LimitReader(r.Body, nodeReportMaxBodyBytes+1))
	if readErr != nil {
		BadRequest(w, "invalid Zero event body")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	recorded := httptest.NewRecorder()
	h.ZeroEventHandler(recorded, r)
	if recorded.Code < http.StatusOK || recorded.Code >= http.StatusMultipleChoices {
		copyRecordedResponse(w, recorded)
		return
	}

	var event zeroEventEnvelope
	if err := json.Unmarshal(body, &event); err != nil {
		copyRecordedResponse(w, recorded)
		return
	}
	nodeID, ok := zeroEventSourceNodeID(event.SourceID)
	if !ok {
		copyRecordedResponse(w, recorded)
		return
	}
	if err := h.projectPrincipalFlowAcceptedEvent(nodeID, event); err != nil {
		ServerError(w, err)
		return
	}
	copyRecordedResponse(w, recorded)
}

func copyRecordedResponse(w http.ResponseWriter, recorded *httptest.ResponseRecorder) {
	for key, values := range recorded.Header() {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(recorded.Code)
	_, _ = w.Write(recorded.Body.Bytes())
}

func zeroEventSourceNodeID(sourceID string) (uint, bool) {
	value := strings.TrimSpace(sourceID)
	if !strings.HasPrefix(value, "node-") {
		return 0, false
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, "node-"), 10, 64)
	if err != nil || parsed == 0 {
		return 0, false
	}
	return uint(parsed), true
}

func parseZeroPrincipalFlowObservation(event zeroEventEnvelope) (zeroPrincipalFlowObservation, bool, error) {
	if event.EventType != "flow.started" && event.EventType != "flow.completed" {
		return zeroPrincipalFlowObservation{}, false, nil
	}
	var payload map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(event.Payload))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil || payload == nil {
		return zeroPrincipalFlowObservation{}, false, errors.New("invalid Zero flow payload")
	}
	activeValue, activeExists := payload["principal_active_flows"]
	revisionValue, revisionExists := payload["session_registry_revision"]
	observedValue, observedExists := payload["observed_at_unix_ms"]
	if !activeExists && !revisionExists && !observedExists {
		return zeroPrincipalFlowObservation{}, false, nil
	}
	if !activeExists || !revisionExists || !observedExists {
		return zeroPrincipalFlowObservation{}, true, errors.New("Zero Principal flow observation is incomplete")
	}
	active, activeOK := uint64Value(activeValue)
	revision, revisionOK := uint64Value(revisionValue)
	observedMillis, observedOK := int64Value(observedValue)
	if !activeOK || !revisionOK || revision == 0 || !observedOK || observedMillis <= 0 {
		return zeroPrincipalFlowObservation{}, true, errors.New("Zero Principal flow observation has invalid counters or timestamp")
	}
	principal := zeroEventPrincipalKey(event, payload)
	if principal == "" {
		return zeroPrincipalFlowObservation{}, true, errors.New("Zero Principal flow observation has no principal_key")
	}
	if strings.TrimSpace(event.CoreInstanceID) == "" {
		return zeroPrincipalFlowObservation{}, true, errors.New("Zero Principal flow observation has no core_instance_id")
	}
	return zeroPrincipalFlowObservation{
		PrincipalKey:            principal,
		ActiveFlows:             active,
		SessionRegistryRevision: revision,
		ObservedAt:              time.UnixMilli(observedMillis).UTC(),
	}, true, nil
}

func zeroEventPrincipalKey(event zeroEventEnvelope, payload map[string]interface{}) string {
	principal := strings.TrimSpace(event.PrincipalKey)
	for _, source := range []map[string]interface{}{payload, nestedMap(payload, "record")} {
		if source == nil {
			continue
		}
		if auth := nestedMap(source, "auth"); auth != nil {
			if value := strings.TrimSpace(stringValue(auth["principal_key"])); value != "" {
				principal = value
			}
		}
	}
	return principal
}

func nestedMap(value map[string]interface{}, key string) map[string]interface{} {
	if value == nil {
		return nil
	}
	nested, _ := value[key].(map[string]interface{})
	return nested
}

func (h *handlers) projectPrincipalFlowAcceptedEvent(nodeID uint, event zeroEventEnvelope) error {
	switch event.EventType {
	case "engine.started":
		return h.recordPrincipalFlowGenerationBoundary(nodeID, event, false)
	case "engine.stopped":
		return h.recordPrincipalFlowGenerationBoundary(nodeID, event, true)
	case "flow.started", "flow.completed":
		observation, present, err := parseZeroPrincipalFlowObservation(event)
		if err != nil {
			return err
		}
		if !present || isMieruMigrationPrincipal(observation.PrincipalKey) {
			return nil
		}
		return h.persistPrincipalFlowObservation(nodeID, event, observation)
	default:
		return nil
	}
}

func (h *handlers) recordPrincipalFlowGenerationBoundary(nodeID uint, event zeroEventEnvelope, stopped bool) error {
	instanceID := strings.TrimSpace(event.CoreInstanceID)
	if nodeID == 0 || instanceID == "" {
		return nil
	}
	observedAt := zeroEventTime(event, time.Now().UTC()).UTC()
	return h.db.Transaction(func(tx *gorm.DB) error {
		var generation principalFlowNodeGeneration
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", nodeID).First(&generation).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if stopped {
			if errors.Is(err, gorm.ErrRecordNotFound) || generation.CoreInstanceID != instanceID || generation.ClosedAt != nil {
				return nil
			}
			if err := resetPrincipalFlowNodeProjection(tx, nodeID, instanceID, event.EventID, observedAt, "engine_stopped"); err != nil {
				return err
			}
			return tx.Model(&principalFlowNodeGeneration{}).Where("node_id = ?", nodeID).Updates(map[string]interface{}{
				"closed_at":  observedAt,
				"updated_at": time.Now().UTC(),
			}).Error
		}

		if err == nil {
			if generation.CoreInstanceID == instanceID {
				return nil
			}
			if !generation.StartedAt.IsZero() && observedAt.Before(generation.StartedAt) {
				return nil
			}
			if err := resetPrincipalFlowNodeProjection(tx, nodeID, instanceID, event.EventID, observedAt, "generation_reset"); err != nil {
				return err
			}
		}
		next := principalFlowNodeGeneration{
			NodeID:         nodeID,
			CoreInstanceID: instanceID,
			StartedAt:      observedAt,
			UpdatedAt:      time.Now().UTC(),
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "node_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"core_instance_id": instanceID,
				"started_at":       observedAt,
				"closed_at":        nil,
				"updated_at":       next.UpdatedAt,
			}),
		}).Create(&next).Error
	})
}

func (h *handlers) persistPrincipalFlowObservation(nodeID uint, event zeroEventEnvelope, observation zeroPrincipalFlowObservation) error {
	return h.db.Transaction(func(tx *gorm.DB) error {
		currentGeneration, err := ensurePrincipalFlowGeneration(tx, nodeID, event, observation.ObservedAt)
		if err != nil {
			return err
		}

		credential, credentialFound, err := resolvePrincipalFlowCredential(tx, nodeID, observation.PrincipalKey)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		raw := principalFlowObservation{
			NodeID:                  nodeID,
			CoreInstanceID:          strings.TrimSpace(event.CoreInstanceID),
			SessionRegistryRevision: observation.SessionRegistryRevision,
			EventID:                 strings.TrimSpace(event.EventID),
			Sequence:                event.Sequence,
			PrincipalKey:            observation.PrincipalKey,
			ActiveFlows:             observation.ActiveFlows,
			ObservedAt:              observation.ObservedAt,
			CreatedAt:               now,
		}
		if credentialFound {
			raw.UserID = credential.UserID
			raw.SubscriptionID = credential.SubscriptionID
			raw.ProtocolCredentialID = credential.ID
			raw.ProtocolEndpointID = credential.ProtocolEndpointID
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&raw)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 || !currentGeneration {
			return nil
		}

		var current principalFlowCurrent
		currentErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("node_id = ? AND principal_key = ?", nodeID, observation.PrincipalKey).
			First(&current).Error
		if currentErr != nil && !errors.Is(currentErr, gorm.ErrRecordNotFound) {
			return currentErr
		}
		if currentErr == nil && current.CoreInstanceID == raw.CoreInstanceID && current.SessionRegistryRevision >= raw.SessionRegistryRevision {
			return nil
		}
		next := principalFlowCurrent{
			NodeID:                  nodeID,
			PrincipalKey:            observation.PrincipalKey,
			CoreInstanceID:          raw.CoreInstanceID,
			SessionRegistryRevision: raw.SessionRegistryRevision,
			UserID:                  raw.UserID,
			SubscriptionID:          raw.SubscriptionID,
			ProtocolCredentialID:    raw.ProtocolCredentialID,
			ProtocolEndpointID:      raw.ProtocolEndpointID,
			ActiveFlows:             raw.ActiveFlows,
			ObservedAt:              raw.ObservedAt,
			UpdatedAt:               now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "node_id"}, {Name: "principal_key"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"core_instance_id", "session_registry_revision", "user_id", "subscription_id",
				"protocol_credential_id", "protocol_endpoint_id", "active_flows", "observed_at", "updated_at",
			}),
		}).Create(&next).Error; err != nil {
			return err
		}
		if !credentialFound {
			return nil
		}
		if raw.UserID > 0 {
			if err := refreshPrincipalFlowScope(tx, principalFlowScopeUser, raw.UserID, nodeID, raw.CoreInstanceID, raw.SessionRegistryRevision, raw.EventID, raw.ObservedAt, "lifecycle"); err != nil {
				return err
			}
		}
		if raw.SubscriptionID > 0 {
			if err := refreshPrincipalFlowScope(tx, principalFlowScopeSubscription, raw.SubscriptionID, nodeID, raw.CoreInstanceID, raw.SessionRegistryRevision, raw.EventID, raw.ObservedAt, "lifecycle"); err != nil {
				return err
			}
		}
		return nil
	})
}

func ensurePrincipalFlowGeneration(tx *gorm.DB, nodeID uint, event zeroEventEnvelope, observedAt time.Time) (bool, error) {
	instanceID := strings.TrimSpace(event.CoreInstanceID)
	if instanceID == "" {
		return false, nil
	}
	var generation principalFlowNodeGeneration
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", nodeID).First(&generation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		next := principalFlowNodeGeneration{NodeID: nodeID, CoreInstanceID: instanceID, StartedAt: observedAt, UpdatedAt: time.Now().UTC()}
		return true, tx.Create(&next).Error
	}
	if err != nil {
		return false, err
	}
	if generation.CoreInstanceID == instanceID {
		if generation.ClosedAt != nil {
			return false, nil
		}
		return true, nil
	}
	if !generation.StartedAt.IsZero() && observedAt.Before(generation.StartedAt) {
		return false, nil
	}
	if err := resetPrincipalFlowNodeProjection(tx, nodeID, instanceID, event.EventID, observedAt, "generation_reset"); err != nil {
		return false, err
	}
	if err := tx.Model(&principalFlowNodeGeneration{}).Where("node_id = ?", nodeID).Updates(map[string]interface{}{
		"core_instance_id": instanceID,
		"started_at":       observedAt,
		"closed_at":        nil,
		"updated_at":       time.Now().UTC(),
	}).Error; err != nil {
		return false, err
	}
	return true, nil
}

func resolvePrincipalFlowCredential(tx *gorm.DB, nodeID uint, principalKey string) (principalFlowCredentialProjection, bool, error) {
	var credential principalFlowCredentialProjection
	err := tx.Table("protocol_credentials").
		Select("id, user_id, subscription_id, protocol_endpoint_id").
		Where("node_id = ? AND principal_key = ?", nodeID, principalKey).
		Order("id DESC").
		Take(&credential).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return principalFlowCredentialProjection{}, false, nil
	}
	return credential, err == nil, err
}

func resetPrincipalFlowNodeProjection(tx *gorm.DB, nodeID uint, coreInstanceID, eventID string, observedAt time.Time, source string) error {
	var currents []principalFlowCurrent
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", nodeID).Find(&currents).Error; err != nil {
		return err
	}
	if len(currents) == 0 {
		return nil
	}
	users := make(map[uint]struct{})
	subscriptions := make(map[uint]struct{})
	for _, current := range currents {
		if current.UserID > 0 {
			users[current.UserID] = struct{}{}
		}
		if current.SubscriptionID > 0 {
			subscriptions[current.SubscriptionID] = struct{}{}
		}
	}
	if err := tx.Where("node_id = ?", nodeID).Delete(&principalFlowCurrent{}).Error; err != nil {
		return err
	}
	for userID := range users {
		if err := refreshPrincipalFlowScope(tx, principalFlowScopeUser, userID, nodeID, coreInstanceID, 0, eventID, observedAt, source); err != nil {
			return err
		}
	}
	for subscriptionID := range subscriptions {
		if err := refreshPrincipalFlowScope(tx, principalFlowScopeSubscription, subscriptionID, nodeID, coreInstanceID, 0, eventID, observedAt, source); err != nil {
			return err
		}
	}
	return nil
}

func refreshPrincipalFlowScope(tx *gorm.DB, scopeType string, scopeID, nodeID uint, coreInstanceID string, revision uint64, eventID string, observedAt time.Time, source string) error {
	if scopeID == 0 {
		return nil
	}
	query := tx.Model(&principalFlowCurrent{})
	switch scopeType {
	case principalFlowScopeUser:
		query = query.Where("user_id = ?", scopeID)
	case principalFlowScopeSubscription:
		query = query.Where("subscription_id = ?", scopeID)
	default:
		return fmt.Errorf("unsupported Principal flow scope %q", scopeType)
	}
	var total struct {
		ActiveFlows uint64 `gorm:"column:active_flows"`
	}
	if err := query.Select("COALESCE(SUM(active_flows), 0) AS active_flows").Scan(&total).Error; err != nil {
		return err
	}
	now := time.Now().UTC()
	current := principalFlowScopeCurrent{ScopeType: scopeType, ScopeID: scopeID, ActiveFlows: total.ActiveFlows, UpdatedAt: now}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "scope_type"}, {Name: "scope_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"active_flows", "updated_at"}),
	}).Create(&current).Error; err != nil {
		return err
	}
	observation := principalFlowScopeObservation{
		ScopeType:               scopeType,
		ScopeID:                 scopeID,
		ActiveFlows:             total.ActiveFlows,
		NodeID:                  nodeID,
		CoreInstanceID:          strings.TrimSpace(coreInstanceID),
		SessionRegistryRevision: revision,
		EventID:                 strings.TrimSpace(eventID),
		Source:                  source,
		ObservedAt:              observedAt.UTC(),
		CreatedAt:               now,
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&observation).Error
}

// TrafficTrendsWithPrincipalFlowsHandler preserves the existing traffic trend
// response and enriches its previously-reserved connection fields from Zboard's
// own user/subscription aggregate observations. Old Core versions simply never
// produce these rows, so their response remains connection_sample_count=0 and
// peak_connections=null without version-string branching.
func (h *handlers) TrafficTrendsWithPrincipalFlowsHandler(w http.ResponseWriter, r *http.Request) {
	recorded := httptest.NewRecorder()
	h.TrafficTrendsHandler(recorded, r)
	if recorded.Code != http.StatusOK {
		copyRecordedResponse(w, recorded)
		return
	}
	var wire struct {
		Code      int             `json:"code"`
		Message   string          `json:"message"`
		Data      json.RawMessage `json:"data"`
		Error     *APIError       `json:"error,omitempty"`
		Timestamp string          `json:"timestamp"`
	}
	if err := json.Unmarshal(recorded.Body.Bytes(), &wire); err != nil {
		copyRecordedResponse(w, recorded)
		return
	}
	var response trafficTrendResponse
	if err := json.Unmarshal(wire.Data, &response); err != nil {
		copyRecordedResponse(w, recorded)
		return
	}
	scopeType, scopeID, allowed, err := h.principalFlowTrendScope(r)
	if err != nil {
		ServerError(w, err)
		return
	}
	if !allowed || scopeID == 0 {
		copyRecordedResponse(w, recorded)
		return
	}
	from, parseErr := time.Parse("2006-01-02", response.From)
	if parseErr != nil {
		copyRecordedResponse(w, recorded)
		return
	}
	to, parseErr := time.Parse("2006-01-02", response.To)
	if parseErr != nil {
		copyRecordedResponse(w, recorded)
		return
	}
	var rows []principalFlowScopeTrendRow
	if err := h.db.Table("principal_flow_scope_observations").
		Select("DATE(observed_at) AS day, MAX(active_flows) AS peak, COUNT(*) AS sample_count").
		Where("scope_type = ? AND scope_id = ? AND observed_at >= ? AND observed_at < ?", scopeType, scopeID, from, to.AddDate(0, 0, 1)).
		Group("DATE(observed_at)").
		Order("day ASC").
		Scan(&rows).Error; err != nil {
		ServerError(w, err)
		return
	}
	applyPrincipalFlowTrendRows(&response, rows)
	writeJSONResponse(w, http.StatusOK, wire.Message, response, wire.Error)
}

func (h *handlers) principalFlowTrendScope(r *http.Request) (string, uint, bool, error) {
	subscriptionID, err := positiveQueryID(r.URL.Query(), "subscription_id")
	if err != nil {
		return "", 0, false, err
	}
	adminRequest := strings.HasPrefix(r.URL.Path, "/api/v1/admin/")
	claims, err := h.authFromRequest(r)
	if err != nil {
		return "", 0, false, err
	}
	if subscriptionID > 0 {
		if !adminRequest {
			var count int64
			if err := h.db.Model(&model.Subscription{}).Where("id = ? AND user_id = ?", subscriptionID, claims.UserID).Count(&count).Error; err != nil {
				return "", 0, false, err
			}
			if count == 0 {
				return "", 0, false, nil
			}
		}
		return principalFlowScopeSubscription, subscriptionID, true, nil
	}
	if adminRequest {
		userID, err := positiveQueryID(r.URL.Query(), "user_id")
		if err != nil {
			return "", 0, false, err
		}
		if userID == 0 {
			return "", 0, false, nil
		}
		return principalFlowScopeUser, userID, true, nil
	}
	return principalFlowScopeUser, claims.UserID, true, nil
}

func applyPrincipalFlowTrendRows(response *trafficTrendResponse, rows []principalFlowScopeTrendRow) {
	if response == nil || len(rows) == 0 {
		return
	}
	byDay := make(map[string]principalFlowScopeTrendRow, len(rows))
	var peak *int64
	var samples int64
	for _, row := range rows {
		key := strings.TrimSpace(row.Day)
		if len(key) >= 10 {
			key = key[:10]
		}
		byDay[key] = row
		samples += row.SampleCount
		value := row.Peak
		if peak == nil || value > *peak {
			copyValue := value
			peak = &copyValue
		}
	}
	for index := range response.Points {
		row, exists := byDay[response.Points[index].Date]
		if !exists {
			continue
		}
		value := row.Peak
		response.Points[index].PeakConnections = &value
	}
	response.ConnectionSampleCount = samples
	response.PeakConnections = peak
}
