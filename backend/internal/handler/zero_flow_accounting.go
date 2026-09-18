package handler

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"sort"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

const zeroRuntimeFlowUsagePrefix = "v2:"

type zeroFlowAccountingResult = metering.FlowAccountingResult

type zeroBufferedFlowKey struct {
	NodeID         uint64
	CoreInstanceID string
	FlowID         string
}

func zeroFlowUsageKey(instance, flow string) string { return metering.FlowUsageKey(instance, flow) }
func zeroFlowUsageRuntimeScoped(key string) bool    { return metering.RuntimeScopedFlow(key) }

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

func pickZeroFlowUsage(candidates []model.FlowUsage, key, legacy string) (model.FlowUsage, bool, bool) {
	return meteringstore.PickFlowUsage(candidates, key, legacy)
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
