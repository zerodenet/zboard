package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const zeroRuntimeFlowUsagePrefix = "v2:"

type zeroFlowAccountingResult struct {
	NodeID             uint
	ProtocolEndpointID uint
	Exhausted          bool
}

type zeroBufferedFlowKey struct {
	NodeID         uint64
	CoreInstanceID string
	FlowID         string
}

func zeroFlowUsageKey(coreInstanceID, flowID string) string {
	flowID = strings.TrimSpace(flowID)
	if flowID == "" {
		return ""
	}
	coreInstanceID = strings.TrimSpace(coreInstanceID)
	if coreInstanceID == "" {
		return flowID
	}
	digest := sha256.Sum256([]byte(coreInstanceID + "\x00" + flowID))
	return zeroRuntimeFlowUsagePrefix + hex.EncodeToString(digest[:])
}

func zeroFlowUsageRuntimeScoped(flowID string) bool {
	return strings.HasPrefix(strings.TrimSpace(flowID), zeroRuntimeFlowUsagePrefix)
}

func aggregateZeroFlowEvents(events []zeroevent.Envelope) []zeroevent.Envelope {
	latest := make(map[zeroBufferedFlowKey]zeroevent.Envelope)
	for _, event := range events {
		if event.Type != "flow.updated" || event.NodeID == 0 {
			continue
		}
		flowID := strings.TrimSpace(event.FlowID)
		if flowID == "" {
			flowID = zeroBufferedFlowID(event.Payload)
		}
		if flowID == "" {
			continue
		}
		key := zeroBufferedFlowKey{
			NodeID:         event.NodeID,
			CoreInstanceID: strings.TrimSpace(event.CoreInstanceID),
			FlowID:         flowID,
		}
		current, exists := latest[key]
		if !exists || zeroEventNewer(event, current) {
			event.FlowID = flowID
			latest[key] = event
		}
	}
	keys := make([]zeroBufferedFlowKey, 0, len(latest))
	for key := range latest {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].NodeID != keys[j].NodeID {
			return keys[i].NodeID < keys[j].NodeID
		}
		if keys[i].CoreInstanceID != keys[j].CoreInstanceID {
			return keys[i].CoreInstanceID < keys[j].CoreInstanceID
		}
		return keys[i].FlowID < keys[j].FlowID
	})
	result := make([]zeroevent.Envelope, 0, len(keys))
	for _, key := range keys {
		result = append(result, latest[key])
	}
	return result
}

func zeroBufferedEnvelopeAsEvent(event zeroevent.Envelope) zeroEventEnvelope {
	occurredAtMillis := int64(0)
	if !event.OccurredAt.IsZero() {
		occurredAtMillis = event.OccurredAt.UTC().UnixMilli()
	}
	return zeroEventEnvelope{
		SchemaID:             "zero.event.v1",
		EventID:              strings.TrimSpace(event.ID),
		EventType:            event.Type,
		OccurredAtUnixMillis: occurredAtMillis,
		SourceID:             strings.TrimSpace(event.SourceID),
		PrincipalKey:         strings.TrimSpace(event.PrincipalKey),
		CoreInstanceID:       strings.TrimSpace(event.CoreInstanceID),
		ConfigRevision:       event.ConfigRevision,
		Sequence:             event.Sequence,
		Payload:              event.Payload,
	}
}

func loadZeroFlowUsage(tx *gorm.DB, nodeID uint, event zeroEventEnvelope, flow zeroFlowProjection, credentialID uint, cumulativeRaw int64) (model.FlowUsage, bool, bool, error) {
	usageKey := zeroFlowUsageKey(event.CoreInstanceID, flow.FlowID)
	var usage model.FlowUsage
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("node_id = ? AND flow_id = ?", nodeID, usageKey).
		First(&usage).Error
	if err == nil {
		return usage, true, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.FlowUsage{}, false, false, err
	}
	if usageKey == flow.FlowID {
		return model.FlowUsage{}, false, false, nil
	}

	// Deploying the runtime-scoped cursor while a connection is already active
	// must not charge the cumulative bytes again. Carry an old raw-flow cursor
	// forward only when ownership and counters are clearly monotonic.
	var legacy model.FlowUsage
	err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("node_id = ? AND flow_id = ?", nodeID, flow.FlowID).
		First(&legacy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.FlowUsage{}, false, false, nil
	}
	if err != nil {
		return model.FlowUsage{}, false, false, err
	}
	if legacy.Status != "active" || legacy.ProtocolCredentialID != credentialID ||
		legacy.RawBytes > cumulativeRaw || legacy.UploadBytes > flow.BytesUp || legacy.DownloadBytes > flow.BytesDown ||
		(flow.Revision > 0 && legacy.Revision > flow.Revision) {
		return model.FlowUsage{}, false, false, nil
	}
	return legacy, true, true, nil
}

func zeroFlowUsageCursorSequence(event zeroEventEnvelope, flow zeroFlowProjection) uint64 {
	if strings.TrimSpace(event.CoreInstanceID) != "" {
		return event.Sequence
	}
	return flow.Revision
}

func zeroRuntimeFlowEventIsStale(usage model.FlowUsage, event zeroEventEnvelope, _ zeroFlowProjection) bool {
	if !zeroFlowUsageRuntimeScoped(usage.FlowID) || usage.Revision == 0 || event.Sequence == 0 {
		return false
	}
	return event.Sequence <= usage.Revision
}

func zeroRuntimeFlowCountersRegress(usage model.FlowUsage, cumulativeRaw int64, flow zeroFlowProjection) bool {
	return usage.RawBytes > cumulativeRaw || usage.UploadBytes > flow.BytesUp || usage.DownloadBytes > flow.BytesDown
}

func (h *handlers) projectBufferedZeroFlow(tx *gorm.DB, buffered zeroevent.Envelope) (zeroFlowAccountingResult, error) {
	var result zeroFlowAccountingResult
	event := zeroBufferedEnvelopeAsEvent(buffered)
	flow, err := parseZeroFlowProjection(event)
	if err != nil {
		return result, err
	}
	if flow.PrincipalKey == "" {
		return result, errors.New("flow event has no attributable principal_key")
	}
	if isMieruMigrationPrincipal(flow.PrincipalKey) {
		return result, nil
	}
	result.NodeID = uint(buffered.NodeID)

	var existing model.TrafficRecord
	if err := tx.Where("node_id = ? AND report_id = ?", result.NodeID, event.EventID).First(&existing).Error; err == nil {
		result.ProtocolEndpointID = existing.ProtocolEndpointID
		return result, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return result, err
	}

	credential, err := h.resolveZeroCompletionCredential(tx, result.NodeID, flow.PrincipalKey)
	if err != nil {
		return result, err
	}
	flow.PrincipalKey = credential.PrincipalKey
	result.ProtocolEndpointID = credential.ProtocolEndpointID

	var subscription model.Subscription
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&subscription, credential.SubscriptionID).Error; err != nil {
		return result, err
	}
	var endpoint model.ProtocolEndpoint
	if err := tx.First(&endpoint, credential.ProtocolEndpointID).Error; err != nil {
		return result, err
	}

	cumulativeRaw := trafficBytesForMode(flow.BytesUp, flow.BytesDown, subscription.TrafficCalcMode)
	usage, found, legacy, err := loadZeroFlowUsage(tx, result.NodeID, event, flow, credential.ID, cumulativeRaw)
	if err != nil {
		return result, err
	}
	if found && usage.Status == "completed" {
		return result, nil
	}
	if found && !legacy {
		if usage.ProtocolCredentialID != credential.ID {
			return result, errors.New("flow principal changed during its runtime generation")
		}
		if zeroRuntimeFlowEventIsStale(usage, event, flow) {
			return result, nil
		}
		if zeroRuntimeFlowCountersRegress(usage, cumulativeRaw, flow) {
			return result, errors.New("Zero flow cumulative counters regressed within one core instance")
		}
	}

	previousRaw, previousUpload, previousDownload := int64(0), int64(0), int64(0)
	if found {
		previousRaw = usage.RawBytes
		previousUpload = usage.UploadBytes
		previousDownload = usage.DownloadBytes
	}
	deltaRaw := cumulativeRaw - previousRaw
	deltaUpload := flow.BytesUp - previousUpload
	deltaDownload := flow.BytesDown - previousDownload
	if deltaRaw < 0 || deltaUpload < 0 || deltaDownload < 0 {
		return result, errors.New("Zero flow cumulative counters cannot produce a negative delta")
	}
	billedDelta, err := billedTrafficBytesChecked(deltaRaw, endpoint.MultiplierMilli)
	if err != nil {
		return result, err
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
	recordAt := zeroEventTime(event, now)
	record := model.TrafficRecord{
		UserID:                  subscription.UserID,
		SubscriptionID:          subscription.ID,
		NodeID:                  result.NodeID,
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
		At:                      recordAt,
		Meta:                    fmt.Sprintf(`{"source_id":%q,"core_instance_id":%q,"sequence":%d,"credential_id":%q,"buffered":true}`, event.SourceID, event.CoreInstanceID, event.Sequence, credential.CredentialID),
	}
	if err := tx.Create(&record).Error; err != nil {
		return result, err
	}

	if charged > 0 {
		subscription.FlowUsed += charged
		if subscription.FlowUsed >= subscription.FlowTotal {
			subscription.FlowUsed = subscription.FlowTotal
			subscription.Status = subStatusExpired
			result.Exhausted = true
		}
		if err := tx.Model(&subscription).Updates(map[string]interface{}{
			"flow_used":  subscription.FlowUsed,
			"status":     subscription.Status,
			"updated_at": now,
		}).Error; err != nil {
			return result, err
		}
	}

	usageKey := zeroFlowUsageKey(event.CoreInstanceID, flow.FlowID)
	usage.ProtocolCredentialID = credential.ID
	usage.NodeID = result.NodeID
	usage.FlowID = usageKey
	usage.SubscriptionID = subscription.ID
	usage.ProtocolEndpointID = endpoint.ID
	usage.PrincipalKey = credential.PrincipalKey
	usage.Revision = zeroFlowUsageCursorSequence(event, flow)
	usage.RawBytes = cumulativeRaw
	usage.UploadBytes = flow.BytesUp
	usage.DownloadBytes = flow.BytesDown
	usage.UsedBytes += charged
	usage.Status = "active"
	usage.LastEventID = event.EventID
	usage.LastSeenAt = recordAt
	usage.CompletedAt = nil
	if found {
		if err := tx.Save(&usage).Error; err != nil {
			return result, err
		}
	} else if err := tx.Create(&usage).Error; err != nil {
		return result, err
	}

	if err := tx.Model(&credential).Updates(map[string]interface{}{"last_used_at": now, "updated_at": now}).Error; err != nil {
		return result, err
	}
	if result.Exhausted {
		if err := tx.Model(&model.ProtocolCredential{}).
			Where("subscription_id = ? AND status IN ?", subscription.ID, []string{protocolCredentialStatusActive, protocolCredentialStatusPrepared}).
			Updates(map[string]interface{}{"status": "expired", "updated_at": now}).Error; err != nil {
			return result, err
		}
	}
	return result, nil
}
