package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
)

const (
	principalFlowScopeUser         = meteringstore.ScopeUser
	principalFlowScopeSubscription = meteringstore.ScopeSubscription
)

// Legacy event ingestion and trend queries share the metering-owned schema.
// These aliases remain until those entrypoints migrate to metering capabilities.
type principalFlowObservation = meteringstore.PrincipalFlowObservation
type principalFlowCurrent = meteringstore.PrincipalFlowCurrent
type principalFlowNodeGeneration = meteringstore.PrincipalFlowNodeGeneration
type principalFlowScopeCurrent = meteringstore.PrincipalFlowScopeCurrent
type principalFlowScopeObservation = meteringstore.PrincipalFlowScopeObservation

type zeroPrincipalFlowObservation struct {
	PrincipalKey            string
	ActiveFlows             uint64
	SessionRegistryRevision uint64
	ObservedAt              time.Time
}

type principalFlowCredentialProjection = meteringstore.CredentialProjection

type principalFlowScopeTrendRow = metering.PrincipalTrendRow

// ZeroEventObservabilityHandler decorates the existing accounting receiver.
// The existing handler remains authoritative for authentication, traffic
// settlement and connector activity. Only after that path accepts an event do
// we project additive Principal connection observations. Returning a 5xx after
// a projection failure is safe because the existing accounting path is
// idempotent and Core will retry its durable lifecycle fact.
func (h *handlers) zeroEventObservabilityHandler(w http.ResponseWriter, r *http.Request) {
	state := zeroEventStateFromContext(r.Context())
	if state == nil {
		var err error
		state, err = h.prepareZeroEventRequest(r)
		if err != nil {
			writeZeroEventRequestError(w, err)
			return
		}
		r = withZeroEventState(r, state)
	}
	recorded := httptest.NewRecorder()
	h.zeroEventHandler(recorded, r)
	if recorded.Code < http.StatusOK || recorded.Code >= http.StatusMultipleChoices {
		copyRecordedResponse(w, recorded)
		return
	}

	event := state.event
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

func principalEvent(event zeroEventEnvelope, observedAt time.Time) metering.PrincipalEvent {
	return metering.PrincipalEvent{CoreInstanceID: event.CoreInstanceID, EventID: event.EventID, Sequence: event.Sequence, ObservedAt: observedAt}
}
func (h *handlers) recordPrincipalFlowGenerationBoundary(node uint, event zeroEventEnvelope, stopped bool) error {
	return h.services.PrincipalCollection.Boundary(context.Background(), node, principalEvent(event, zeroEventTime(event, time.Now().UTC())), stopped)
}
func (h *handlers) persistPrincipalFlowObservation(node uint, event zeroEventEnvelope, observation zeroPrincipalFlowObservation) error {
	return h.services.PrincipalCollection.Observe(context.Background(), node, principalEvent(event, observation.ObservedAt), metering.PrincipalObservation(observation))
}

// TrafficTrendsWithPrincipalFlowsHandler preserves the existing traffic trend
// response and enriches its previously-reserved connection fields from Zboard's
// own user/subscription aggregate observations. Old Core versions simply never
// produce these rows, so their response remains connection_sample_count=0 and
// peak_connections=null without version-string branching.
func (h *handlers) TrafficTrendsWithPrincipalFlowsHandler(w http.ResponseWriter, r *http.Request) {
	recorded := httptest.NewRecorder()
	h.trafficTrends(recorded, r, false)
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
	days, err := inclusiveSystemDayCount(from, to, trafficTrendMaxDays)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	rows, err := h.readPrincipalTrends(r, systemDayBuckets(from, days, time.UTC))
	if err != nil {
		writePrincipalTrendError(w, err)
		return
	}
	applyPrincipalFlowTrendRows(&response, rows)
	writeJSONResponse(w, http.StatusOK, wire.Message, response, wire.Error)
}

func (h *handlers) readPrincipalTrends(r *http.Request, buckets []systemCalendarBucket) ([]principalFlowScopeTrendRow, error) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		return nil, err
	}
	subscription, err := positiveQueryID(r.URL.Query(), "subscription_id")
	if err != nil {
		return nil, err
	}
	admin := strings.HasPrefix(r.URL.Path, "/api/v1/admin/")
	var user uint
	if admin {
		user, err = positiveQueryID(r.URL.Query(), "user_id")
		if err != nil {
			return nil, err
		}
	}
	converted := make([]metering.TrendBucket, len(buckets))
	for i, b := range buckets {
		converted[i] = metering.TrendBucket{Key: b.Key, StartUTC: b.StartUTC, EndUTC: b.EndUTC}
	}
	return h.services.PrincipalTrends().Read(r.Context(), claims.UserID, metering.PrincipalTrendQuery{Administrative: admin, UserID: user, SubscriptionID: subscription, Buckets: converted})
}
func writePrincipalTrendError(w http.ResponseWriter, err error) {
	if errors.Is(err, metering.ErrTrendPermission) {
		Forbidden(w, "当前账号无权读取此趋势。")
		return
	}
	writeFairUsePolicyError(w, err)
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
