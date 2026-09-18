package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"
)

const (
	fairUseDefaultConnectionStartWindowSeconds = 60
	fairUseDefaultWorkingNodeWindowSeconds     = 300
	fairUseMinConnectionStartWindowSeconds     = 10
	fairUseMinWorkingNodeWindowSeconds         = 30
	fairUseMaxTelemetryWindowSeconds           = 3600
)

type subscriptionFlowStartEvent = meteringstore.FlowStartRecord
type fairUseWindowMetric = metering.WindowMetric
type fairUseTelemetryMetrics = metering.TelemetryMetrics

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
	h.zeroEventObservabilityHandler(recorded, r)
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
	principal := flowStartedPrincipalKey(event)
	if isMieruMigrationPrincipal(principal) {
		return nil
	}
	now := time.Now().UTC()
	return h.services.FairUseFlowCollection.Record(context.Background(), metering.FlowStart{NodeID: nodeID, CoreInstanceID: event.CoreInstanceID, EventID: event.EventID, Sequence: event.Sequence, PrincipalKey: principal, OccurredAt: zeroEventTime(event, now), ReceivedAt: now})
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

// AdminSubscriptionFairUseMetricsHandler exposes both diagnostic event-time
// and receive-time signals. Business evaluation uses receive-time windows only:
// Core clock skew or spool backlog must not look like subscriber behaviour.
func (h *handlers) AdminSubscriptionFairUseMetricsHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
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

	metrics, err := h.services.FairUseTelemetry.Read(r.Context(), actor.UserID, subscriptionID, connectionWindow, workingNodeWindow, time.Now().UTC())
	if err != nil {
		writeFairUsePolicyError(w, err)
		return
	}
	OK(w, metrics)
}
