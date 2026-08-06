package handler

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/zerodenet/zboard/backend/internal/model"
)

type zeroEventEnvelope struct {
	SchemaID             string          `json:"schema_id"`
	EventID              string          `json:"event_id"`
	EventType            string          `json:"event_type"`
	OccurredAtUnixMillis int64           `json:"occurred_at_unix_ms"`
	SourceID             string          `json:"source_id"`
	PrincipalKey         string          `json:"principal_key"`
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

// zeroCompletionAccountingBaseline carries forward only an active, monotonic
// cursor from the same credential. Completed, reset, reused, or regressed flow
// identifiers are treated as a new connection and start from zero.
func zeroCompletionAccountingBaseline(usage model.FlowUsage, found bool, credentialID uint, flow zeroFlowProjection, cumulativeRaw int64) zeroCompletionBaseline {
	if !found || usage.Status != "active" || usage.ProtocolCredentialID != credentialID {
		return zeroCompletionBaseline{}
	}
	if usage.RawBytes > cumulativeRaw || usage.UploadBytes > flow.BytesUp || usage.DownloadBytes > flow.BytesDown {
		return zeroCompletionBaseline{}
	}
	if flow.Revision > 0 && usage.Revision > flow.Revision {
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
	body, err := io.ReadAll(io.LimitReader(r.Body, nodeReportMaxBodyBytes+1))
	if err != nil || len(body) == 0 || len(body) > nodeReportMaxBodyBytes {
		BadRequest(w, "invalid Zero event body")
		return
	}
	var event zeroEventEnvelope
	if err := json.Unmarshal(body, &event); err != nil {
		BadRequest(w, "invalid Zero event JSON")
		return
	}
	if event.SchemaID != "zero.event.v1" || strings.TrimSpace(event.EventID) == "" {
		BadRequest(w, "unsupported Zero event envelope")
		return
	}
	node, err := h.authenticateZeroEvent(r, event.SourceID)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	if event.EventType == "flow.updated" {
		if err := h.recordZeroConnectorActivity(node, event); err != nil {
			ServerError(w, err)
			return
		}
		OK(w, map[string]interface{}{
			"accepted": true,
			"ignored":  true,
			"reason":   "flow.updated is an observability event",
		})
		return
	}
	if !isZeroAccountingEvent(event.EventType) {
		if err := h.recordZeroConnectorActivity(node, event); err != nil {
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
		if err := h.recordZeroConnectorActivity(node, event); err != nil {
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
	if err := h.recordZeroConnectorActivity(node, event); err != nil {
		ServerError(w, err)
		return
	}
	if exhausted {
		h.scheduleNodeConfigPublish(node.ID, result.ProtocolEndpointID, 0)
	}
	OK(w, map[string]interface{}{
		"accepted":          true,
		"event_id":          event.EventID,
		"traffic_record_id": result.ID,
		"charged_bytes":     result.UsedBytes,
	})
}

func isMieruMigrationPrincipal(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "migration:endpoint:") {
		return false
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(value, "migration:endpoint:"), 10, 64)
	return err == nil && id > 0
}

func (h *handlers) authenticateZeroEvent(r *http.Request, sourceID string) (model.Node, error) {
	if !strings.HasPrefix(sourceID, "node-") {
		return model.Node{}, errors.New("invalid Zero event source_id")
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(sourceID, "node-"), 10, 64)
	if err != nil || id == 0 {
		return model.Node{}, errors.New("invalid Zero event source_id")
	}
	provided, err := extractBearerToken(r)
	if err != nil {
		return model.Node{}, errors.New("missing Zero event bearer credential")
	}
	var node model.Node
	if err := h.db.First(&node, uint(id)).Error; err != nil || node.NodeCredentialRevokedAt != nil || node.NodeCredential == "" {
		return model.Node{}, errors.New("Zero event credential is unavailable")
	}
	expected, err := h.credentialCipher.Decrypt(node.NodeCredential)
	if err != nil {
		return model.Node{}, errors.New("Zero event credential is unavailable")
	}
	if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		return model.Node{}, errors.New("invalid Zero event credential")
	}
	return node, nil
}

func (h *handlers) recordZeroConnectorActivity(node model.Node, event zeroEventEnvelope) error {
	now := time.Now().UTC()
	updates := map[string]interface{}{
		"last_seen_at":           now,
		"connector_last_seen_at": now,
		"is_online":              true,
		"status":                 1,
	}
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
			updates["version"] = payload.BuildID
		}
	case "engine.stopped":
		updates["is_online"] = false
		updates["status"] = 0
		updates["connector_last_seen_at"] = nil
	case "stats.sampled":
		stats, err := parseZeroStatsProjection(event.Payload)
		if err != nil {
			return err
		}
		updates["active_flows"] = stats.ActiveSessions
		updates["bytes_up"] = stats.BytesUp
		updates["bytes_down"] = stats.BytesDown
	}
	result := h.db.Model(&model.Node{}).
		Where("id = ? AND is_enabled = ? AND node_credential = ? AND node_credential_revoked_at IS NULL", node.ID, true, node.NodeCredential).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("Zero event credential is no longer active")
	}
	if event.EventType != "engine.stopped" {
		go h.reconcileExpiredCredentials(now)
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

func (h *handlers) recordZeroFlowEvent(node model.Node, event zeroEventEnvelope, flow zeroFlowProjection) (model.TrafficRecord, bool, error) {
	var record model.TrafficRecord
	if !isZeroAccountingEvent(event.EventType) {
		return record, false, fmt.Errorf("Zero event %q is not an accounting event", event.EventType)
	}
	exhausted := false
	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("node_id = ? AND report_id = ?", node.ID, event.EventID).First(&record).Error; err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		credential, credentialErr := h.resolveZeroCompletionCredential(tx, node.ID, flow.PrincipalKey)
		if credentialErr != nil {
			return credentialErr
		}
		flow.PrincipalKey = credential.PrincipalKey
		var subscription model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&subscription, credential.SubscriptionID).Error; err != nil {
			return err
		}
		var endpoint model.ProtocolEndpoint
		if err := tx.First(&endpoint, credential.ProtocolEndpointID).Error; err != nil {
			return err
		}
		var usage model.FlowUsage
		usageErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("node_id = ? AND flow_id = ?", node.ID, flow.FlowID).First(&usage).Error
		if usageErr != nil && !errors.Is(usageErr, gorm.ErrRecordNotFound) {
			return usageErr
		}
		cumulativeRaw := trafficBytesForMode(flow.BytesUp, flow.BytesDown, subscription.TrafficCalcMode)
		baseline := zeroCompletionAccountingBaseline(usage, usageErr == nil, credential.ID, flow, cumulativeRaw)
		deltaRaw := cumulativeRaw - baseline.RawBytes
		deltaUpload := flow.BytesUp - baseline.UploadBytes
		deltaDownload := flow.BytesDown - baseline.DownloadBytes
		billedDelta, err := billedTrafficBytesChecked(deltaRaw, endpoint.MultiplierMilli)
		if err != nil {
			return err
		}
		remaining := subscription.FlowTotal - subscription.FlowUsed
		if remaining < 0 {
			remaining = 0
		}
		charged := billedDelta
		if charged > remaining {
			charged = remaining
		}
		now := time.Now().UTC()
		record = model.TrafficRecord{
			UserID:                  subscription.UserID,
			SubscriptionID:          subscription.ID,
			NodeID:                  node.ID,
			ProtocolEndpointID:      endpoint.ID,
			ReportID:                event.EventID,
			Nonce:                   zeroEventNonce(event.EventID),
			FlowID:                  flow.FlowID,
			EventType:               event.EventType,
			EventRevision:           flow.Revision,
			RawBytes:                deltaRaw,
			UploadBytes:             deltaUpload,
			DownloadBytes:           deltaDownload,
			TrafficCalcMode:         subscription.TrafficCalcMode,
			ProtocolMultiplierMilli: endpoint.MultiplierMilli,
			UsedBytes:               charged,
			At:                      zeroEventTime(event, now),
			Meta:                    fmt.Sprintf(`{"source_id":%q,"sequence":%d,"credential_id":%q,"continued_legacy_flow":%t}`, event.SourceID, event.Sequence, credential.CredentialID, baseline.ContinuesFlow),
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if charged > 0 {
			subscription.FlowUsed += charged
			if subscription.FlowUsed >= subscription.FlowTotal {
				subscription.FlowUsed = subscription.FlowTotal
				subscription.Status = subStatusExpired
				exhausted = true
			}
			if err := tx.Model(&subscription).Updates(map[string]interface{}{"flow_used": subscription.FlowUsed, "status": subscription.Status, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		if usageErr == nil {
			if err := tx.Delete(&usage).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&credential).Updates(map[string]interface{}{"last_used_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		if exhausted {
			if err := tx.Model(&model.ProtocolCredential{}).Where("subscription_id = ? AND status IN ?", subscription.ID,
				[]string{protocolCredentialStatusActive, protocolCredentialStatusPrepared}).
				Updates(map[string]interface{}{"status": "expired", "updated_at": now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return record, exhausted, err
}

// Completion facts remain attributable after a subscription or credential has
// expired. The node event itself is authenticated, so historical credentials
// can be used for final ownership resolution without re-enabling access.
func (h *handlers) resolveZeroCompletionCredential(tx *gorm.DB, nodeID uint, principal string) (model.ProtocolCredential, error) {
	var credential model.ProtocolCredential
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("principal_key = ? AND node_id = ?", principal, nodeID).
		Order("id DESC").
		First(&credential).Error
	if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
		return credential, err
	}
	return h.matchNodeCredentialSecret(tx, nodeID, principal)
}

// Current Zero Shadowsocks events use the authenticated password as the
// protocol principal. Match it only in memory, then immediately replace it
// with the stable panel principal; the password is never persisted in traffic
// records or logs. Completed flows may reference a credential that has already
// expired, so ownership lookup intentionally includes historical credentials.
func (h *handlers) matchNodeCredentialSecret(tx *gorm.DB, nodeID uint, provided string) (model.ProtocolCredential, error) {
	var credentials []model.ProtocolCredential
	if err := tx.Where("node_id = ?", nodeID).Order("id DESC").Find(&credentials).Error; err != nil {
		return model.ProtocolCredential{}, err
	}
	for _, credential := range credentials {
		secret, err := h.credentialCipher.Decrypt(credential.Secret)
		if err != nil {
			continue
		}
		if len(secret) == len(provided) && subtle.ConstantTimeCompare([]byte(secret), []byte(provided)) == 1 {
			return credential, nil
		}
	}
	return model.ProtocolCredential{}, gorm.ErrRecordNotFound
}

func zeroEventNonce(eventID string) string {
	digest := sha256.Sum256([]byte(eventID))
	return hex.EncodeToString(digest[:])
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
