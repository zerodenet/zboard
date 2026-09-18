package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/zerodenet/zboard/backend/internal/model"
)

const (
	zeroEventCredentialCacheTTL        = 5 * time.Second
	zeroEventInvalidCredentialCacheTTL = time.Minute
)

type zeroEventRequestStateKey struct{}

type zeroEventRequestState struct {
	event      zeroEventEnvelope
	node       network.EventNode
	receivedAt time.Time
	buffered   bool
}

type zeroEventCredentialCacheEntry struct {
	node      network.EventNode
	secret    string
	expiresAt time.Time
}

type zeroEventAuthFailureEntry struct {
	digest    [sha256.Size]byte
	expiresAt time.Time
}

type zeroEventEnvelope struct {
	SchemaID             string          `json:"schema_id"`
	EventID              string          `json:"event_id"`
	EventType            string          `json:"event_type"`
	OccurredAtUnixMillis int64           `json:"occurred_at_unix_ms"`
	SourceID             string          `json:"source_id"`
	PrincipalKey         string          `json:"principal_key"`
	CoreInstanceID       string          `json:"core_instance_id"`
	ConfigRevision       uint64          `json:"config_revision"`
	Sequence             uint64          `json:"sequence"`
	Payload              json.RawMessage `json:"payload"`
}

type zeroFlowProjection struct {
	FlowID       string
	Revision     uint64
	PrincipalKey string
	BytesUp      int64
	BytesDown    int64
}

type zeroCompletionBaseline struct {
	RawBytes      int64
	UploadBytes   int64
	DownloadBytes int64
	ContinuesFlow bool
}

func isZeroAccountingEvent(eventType string) bool {
	return eventType == "flow.completed"
}

// zeroCompletionAccountingBaseline remains the compatibility rule for legacy
// raw flow cursors. Runtime-scoped cursors additionally use the event sequence
// in recordZeroFlowEvent so a restarted engine cannot reuse the same position.
func zeroCompletionAccountingBaseline(usage model.FlowUsage, found bool, credentialID uint, flow zeroFlowProjection, cumulativeRaw int64) zeroCompletionBaseline {
	if !found || !metering.ContinuesLegacyFlow(meteringstore.FlowCursor(usage), credentialID, flow.Revision, metering.FlowCounters{Raw: cumulativeRaw, Upload: flow.BytesUp, Download: flow.BytesDown}) {
		return zeroCompletionBaseline{}
	}
	return zeroCompletionBaseline{
		RawBytes:      usage.RawBytes,
		UploadBytes:   usage.UploadBytes,
		DownloadBytes: usage.DownloadBytes,
		ContinuesFlow: true,
	}
}

func (h *handlers) ZeroEventHandler(w http.ResponseWriter, r *http.Request) {
	h.zeroEventHandler(w, r)
}

func (h *handlers) zeroEventHandler(w http.ResponseWriter, r *http.Request) {
	state := zeroEventStateFromContext(r.Context())
	if state == nil {
		var err error
		state, err = h.prepareZeroEventRequest(r)
		if err != nil {
			writeZeroEventRequestError(w, err)
			return
		}
	}
	event, node := state.event, state.node

	// High-frequency buffered events are fully validated before durable append.
	// After validation, any Append error is a storage/service failure and must
	// stay retryable instead of being misclassified as a client-side 400.
	switch event.EventType {
	case "stats.sampled":
		if _, err := parseZeroStatsProjection(event.Payload); err != nil {
			BadRequest(w, err.Error())
			return
		}
	case "flow.updated":
		flow, err := parseZeroFlowProjection(event)
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
		if flow.PrincipalKey == "" {
			BadRequest(w, "flow event has no attributable principal_key")
			return
		}
	}
	if buffered, err := h.appendBufferedZeroEvent(r.Context(), node, event); buffered {
		if err != nil {
			ServerError(w, err)
			return
		}
		state.buffered = true
		OK(w, map[string]interface{}{
			"accepted": true,
			"buffered": true,
			"event_id": event.EventID,
		})
		return
	}

	// Legacy mode intentionally keeps the old synchronous path as the migration
	// rollback switch. Interim flow accounting is disabled there; completion is
	// still authoritative and settles the final cumulative total.
	if event.EventType == "flow.updated" {
		if err := h.recordZeroConnectorActivity(r.Context(), node, event); err != nil {
			ServerError(w, err)
			return
		}
		OK(w, map[string]interface{}{
			"accepted": true,
			"ignored":  true,
			"reason":   "flow.updated is an observability event in legacy mode",
		})
		return
	}
	if !isZeroAccountingEvent(event.EventType) {
		if err := h.recordZeroConnectorActivity(r.Context(), node, event); err != nil {
			ServerError(w, err)
			return
		}
		OK(w, map[string]interface{}{"accepted": true, "ignored": true})
		return
	}
	flow, err := parseZeroFlowProjection(event)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	if flow.PrincipalKey == "" {
		BadRequest(w, "flow event has no attributable principal_key")
		return
	}
	if isMieruMigrationPrincipal(flow.PrincipalKey) {
		if err := h.recordZeroConnectorActivity(r.Context(), node, event); err != nil {
			ServerError(w, err)
			return
		}
		OK(w, map[string]interface{}{"accepted": true, "ignored": true, "reason": "Mieru credential migration"})
		return
	}
	result, exhausted, err := h.recordZeroFlowEvent(node, event, flow)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			BadRequest(w, "flow principal is not known on this node")
			return
		}
		ServerError(w, err)
		return
	}
	if err := h.recordZeroConnectorActivity(r.Context(), node, event); err != nil {
		ServerError(w, err)
		return
	}
	if exhausted {
	}
	OK(w, map[string]interface{}{
		"accepted":          true,
		"event_id":          event.EventID,
		"traffic_record_id": result.ID,
		"charged_bytes":     result.UsedBytes,
	})
}

type zeroEventRequestError struct {
	status  int
	message string
}

func (e *zeroEventRequestError) Error() string { return e.message }

func writeZeroEventRequestError(w http.ResponseWriter, err error) {
	var requestErr *zeroEventRequestError
	if errors.As(err, &requestErr) {
		if requestErr.status == http.StatusUnauthorized {
			Unauthorized(w, requestErr.message)
			return
		}
		BadRequest(w, requestErr.message)
		return
	}
	ServerError(w, err)
}

func (h *handlers) prepareZeroEventRequest(r *http.Request) (*zeroEventRequestState, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, nodeReportMaxBodyBytes+1))
	if err != nil || len(body) == 0 || len(body) > nodeReportMaxBodyBytes {
		return nil, &zeroEventRequestError{status: http.StatusBadRequest, message: "invalid Zero event body"}
	}
	var event zeroEventEnvelope
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, &zeroEventRequestError{status: http.StatusBadRequest, message: "invalid Zero event JSON"}
	}
	if event.SchemaID != "zero.event.v1" || strings.TrimSpace(event.EventID) == "" {
		return nil, &zeroEventRequestError{status: http.StatusBadRequest, message: "unsupported Zero event envelope"}
	}
	node, err := h.authenticateZeroEvent(r, event.SourceID)
	if err != nil {
		return nil, &zeroEventRequestError{status: http.StatusUnauthorized, message: err.Error()}
	}
	return &zeroEventRequestState{
		event: event, node: node, receivedAt: time.Now().UTC(),
	}, nil
}

func zeroEventStateFromContext(ctx context.Context) *zeroEventRequestState {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(zeroEventRequestStateKey{}).(*zeroEventRequestState)
	return state
}

func withZeroEventState(r *http.Request, state *zeroEventRequestState) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), zeroEventRequestStateKey{}, state))
}

func isMieruMigrationPrincipal(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "migration:endpoint:") {
		return false
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(value, "migration:endpoint:"), 10, 64)
	return err == nil && id > 0
}

func (h *handlers) authenticateZeroEvent(r *http.Request, sourceID string) (network.EventNode, error) {
	if !strings.HasPrefix(sourceID, "node-") {
		return network.EventNode{}, errors.New("invalid Zero event source_id")
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(sourceID, "node-"), 10, 64)
	if err != nil || id == 0 {
		return network.EventNode{}, errors.New("invalid Zero event source_id")
	}
	provided, err := extractBearerToken(r)
	if err != nil {
		return network.EventNode{}, errors.New("missing Zero event bearer credential")
	}
	nodeID := uint(id)
	now := time.Now().UTC()
	providedDigest := sha256.Sum256([]byte(provided))
	if cached, ok := h.zeroEventAuthFailures.Load(nodeID); ok {
		entry := cached.(zeroEventAuthFailureEntry)
		if entry.digest == providedDigest && now.Before(entry.expiresAt) {
			return network.EventNode{}, errors.New("invalid Zero event credential")
		}
		if !now.Before(entry.expiresAt) {
			h.zeroEventAuthFailures.Delete(nodeID)
		}
	}
	if cached, ok := h.zeroEventAuthCache.Load(nodeID); ok {
		entry := cached.(zeroEventCredentialCacheEntry)
		if now.Before(entry.expiresAt) && len(provided) == len(entry.secret) && subtle.ConstantTimeCompare([]byte(provided), []byte(entry.secret)) == 1 {
			return entry.node, nil
		}
		if !now.Before(entry.expiresAt) {
			h.zeroEventAuthCache.Delete(nodeID)
		}
	}
	node, err := h.services.EventCredentials.Load(r.Context(), nodeID)
	if err != nil {
		h.zeroEventAuthFailures.Store(nodeID, zeroEventAuthFailureEntry{digest: providedDigest, expiresAt: now.Add(zeroEventInvalidCredentialCacheTTL)})
		return network.EventNode{}, errors.New("Zero event credential is unavailable")
	}
	expected, err := h.credentialCipher.Decrypt(node.Credential)
	if err != nil {
		return network.EventNode{}, errors.New("Zero event credential is unavailable")
	}
	if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		h.zeroEventAuthFailures.Store(nodeID, zeroEventAuthFailureEntry{digest: providedDigest, expiresAt: now.Add(zeroEventInvalidCredentialCacheTTL)})
		return network.EventNode{}, errors.New("invalid Zero event credential")
	}
	h.zeroEventAuthFailures.Delete(nodeID)
	h.zeroEventAuthCache.Store(nodeID, zeroEventCredentialCacheEntry{node: node, secret: expected, expiresAt: now.Add(zeroEventCredentialCacheTTL)})
	return node, nil
}

func (h *handlers) invalidateZeroEventCredential(nodeID uint) {
	if h != nil && nodeID != 0 {
		h.zeroEventAuthCache.Delete(nodeID)
		h.zeroEventAuthFailures.Delete(nodeID)
	}
}

func (h *handlers) recordZeroConnectorActivity(ctx context.Context, node network.EventNode, event zeroEventEnvelope) error {
	now := time.Now().UTC()
	activity := network.NodeActivityUpdate{At: now, Online: true, ConnectorSeen: true}
	switch event.EventType {
	case "engine.started":
		var payload struct {
			BuildID string `json:"build_id"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return errors.New("invalid Zero engine.started payload")
		}
		payload.BuildID = strings.TrimSpace(payload.BuildID)
		if len(payload.BuildID) > 64 {
			return errors.New("Zero engine build_id is too long")
		}
		if payload.BuildID != "" {
			activity.Version = &payload.BuildID
		}
	case "engine.stopped":
		activity.Online = false
		activity.ConnectorSeen = false
	case "stats.sampled":
		stats, err := parseZeroStatsProjection(event.Payload)
		if err != nil {
			return err
		}
		activity.ActiveFlows = &stats.ActiveSessions
		activity.BytesUp = &stats.BytesUp
		activity.BytesDown = &stats.BytesDown
	}
	if err := h.services.NodeActivity.Record(ctx, node.ID, node.Credential, activity); err != nil {
		if !errors.Is(err, network.ErrNodeActivityCredential) {
			return err
		}
		return errors.New("Zero event credential is no longer active")
	}
	return nil
}

type zeroStatsProjection struct {
	ActiveSessions uint64
	BytesUp        uint64
	BytesDown      uint64
}

func parseZeroStatsProjection(payload json.RawMessage) (zeroStatsProjection, error) {
	var value map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil || value == nil {
		return zeroStatsProjection{}, errors.New("invalid Zero stats.sampled payload")
	}
	active, activeOK := uint64Value(value["active_sessions"])
	bytesUp, upOK := uint64Value(value["bytes_up"])
	bytesDown, downOK := uint64Value(value["bytes_down"])
	if !activeOK || !upOK || !downOK {
		return zeroStatsProjection{}, errors.New("Zero stats.sampled payload has invalid counters")
	}
	return zeroStatsProjection{ActiveSessions: active, BytesUp: bytesUp, BytesDown: bytesDown}, nil
}

func parseZeroFlowProjection(event zeroEventEnvelope) (zeroFlowProjection, error) {
	var payload map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(event.Payload))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil || payload == nil {
		return zeroFlowProjection{}, errors.New("invalid Zero flow payload")
	}
	record := payload
	if nested, ok := payload["record"].(map[string]interface{}); ok {
		record = nested
	}
	flowID := stringValue(record["flow_id"])
	if flowID == "" {
		flowID = stringValue(payload["flow_id"])
	}
	if flowID == "" {
		return zeroFlowProjection{}, errors.New("Zero flow event has no flow_id")
	}
	principal := strings.TrimSpace(event.PrincipalKey)
	if auth, ok := record["auth"].(map[string]interface{}); ok {
		if value := strings.TrimSpace(stringValue(auth["principal_key"])); value != "" {
			principal = value
		}
	}
	traffic, _ := record["traffic"].(map[string]interface{})
	if traffic == nil {
		traffic, _ = payload["traffic"].(map[string]interface{})
	}
	bytesUp, okUp := int64Value(traffic["bytes_up"])
	bytesDown, okDown := int64Value(traffic["bytes_down"])
	if !okUp || !okDown || bytesUp < 0 || bytesDown < 0 {
		return zeroFlowProjection{}, errors.New("Zero flow event has invalid traffic totals")
	}
	revision, _ := uint64Value(record["revision"])
	if revision == 0 {
		revision, _ = uint64Value(payload["revision"])
	}
	return zeroFlowProjection{FlowID: flowID, Revision: revision, PrincipalKey: principal, BytesUp: bytesUp, BytesDown: bytesDown}, nil
}

func (h *handlers) recordZeroFlowEvent(node network.EventNode, event zeroEventEnvelope, flow zeroFlowProjection) (model.TrafficRecord, bool, error) {
	record, exhausted, err := h.services.CompletionAccounting(h.credentialCipher).Complete(context.Background(), metering.CompletedFlow{NodeID: node.ID, SourceID: event.SourceID, CoreInstanceID: event.CoreInstanceID, EventID: event.EventID, EventType: event.EventType, Sequence: event.Sequence, FlowID: flow.FlowID, PrincipalKey: flow.PrincipalKey, Revision: flow.Revision, BytesUp: flow.BytesUp, BytesDown: flow.BytesDown, OccurredAt: zeroEventTime(event, time.Now().UTC())})
	return model.TrafficRecord(record), exhausted, err
}
func zeroEventTime(event zeroEventEnvelope, fallback time.Time) time.Time {
	if event.OccurredAtUnixMillis <= 0 {
		return fallback
	}
	return time.UnixMilli(event.OccurredAtUnixMillis).UTC()
}

func stringValue(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	default:
		return ""
	}
}

func int64Value(value interface{}) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		if typed > float64(^uint64(0)>>1) {
			return 0, false
		}
		return int64(typed), true
	case json.Number:
		value, err := typed.Int64()
		return value, err == nil
	case int64:
		return typed, true
	default:
		return 0, false
	}
}

func uint64Value(value interface{}) (uint64, bool) {
	integer, ok := int64Value(value)
	if !ok || integer < 0 {
		return 0, false
	}
	return uint64(integer), true
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func (event zeroEventEnvelope) String() string {
	return fmt.Sprintf("%s/%s", event.EventType, event.EventID)
}
