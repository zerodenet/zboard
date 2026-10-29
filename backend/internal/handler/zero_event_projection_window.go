package handler

import (
	"context"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

const zeroEventReadWindowLimit = 2000

// Read windows and authority transactions have different bounds: the former
// reduces repeated snapshots, while the latter must yield the database to users.
func (h *handlers) projectZeroNodeEventsBounded(ctx context.Context, events []zeroevent.Envelope) (int, error) {
	return h.projectZeroNodeEventsWithMerge(ctx, events, true)
}

func (h *handlers) projectZeroNodeEventsWithMerge(ctx context.Context, events []zeroevent.Envelope, merge bool) (int, error) {
	samples, err := zeroFlowSamplesWithMerge(events, merge)
	if err != nil {
		return 0, err
	}
	limit := zeroEventConsumerBatchLimit(zeroEventReadWindowLimit, h.services.TrafficReadsUseSQLite())
	sources := make([]network.ObservationSource, 0, len(events))
	for _, event := range events {
		if event.NodeID == 0 {
			continue
		}
		o := network.NodeObservation{NodeID: uint(event.NodeID), Latest: networkEventPosition(event)}
		if event.Type == "stats.sampled" {
			if stats, err := parseZeroStatsProjection(event.Payload); err == nil {
				position := o.Latest
				o.StatsEvent = &position
				o.Stats = network.NodeStats{ActiveSessions: stats.ActiveSessions, BytesUp: stats.BytesUp, BytesDown: stats.BytesDown}
			}
		}
		sources = append(sources, network.ObservationSource{Observation: o, Kind: event.Type, PrincipalKey: event.PrincipalKey, FlowID: event.FlowID, Payload: event.Payload})
	}
	for start := 0; start < len(samples); start += limit {
		input := application.BufferedEventProjection{Flows: samples[start:min(start+limit, len(samples))]}
		// Retain the original source for each report/checkpoint settled in this
		// existing authority batch. The financial window/merge/keys are unchanged.
		type report struct {
			node uint
			id   string
		}
		selected := map[report]bool{}
		for _, sample := range input.Flows {
			selected[report{sample.NodeID, sample.EventID}] = true
			for _, point := range sample.Checkpoints {
				selected[report{sample.NodeID, point.ReportID}] = true
			}
		}
		remaining := sources[:0]
		for _, source := range sources {
			if source.Kind == "flow.updated" && selected[report{source.Observation.NodeID, source.Observation.Latest.ID}] {
				input.Sources = append(input.Sources, source)
			} else {
				remaining = append(remaining, source)
			}
		}
		sources = remaining
		if _, err := h.services.ProjectBufferedEvents(ctx, input, h.credentialCipher); err != nil {
			return 0, err
		}
	}
	coverage := make(map[uint][]metering.NodeCoverageEvent)
	for _, event := range events {
		if event.NodeID == 0 {
			continue
		}
		node := uint(event.NodeID)
		coverage[node] = append(coverage[node], coverageInput(node, zeroBufferedEnvelopeAsEvent(event), event.ReceivedAt))
	}
	nodes := make([]uint, 0, len(coverage))
	for node := range coverage {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })
	for start := 0; start < len(nodes); start += limit {
		input := application.BufferedEventProjection{Sources: sources}
		sources = nil
		for _, node := range nodes[start:min(start+limit, len(nodes))] {
			input.Coverage = append(input.Coverage, coverage[node]...)
		}
		result, err := h.services.ProjectBufferedEvents(ctx, input, h.credentialCipher)
		if err != nil {
			return 0, err
		}
		if result.CoverageErr != nil && ctx.Err() == nil {
			log.Printf("fair use buffered coverage projection failed: %v", result.CoverageErr)
		}
	}
	return len(samples), nil
}

type zeroSamplePosition struct {
	assetTag string
	sample   int
	revision uint64
	config   uint64
	minute   int64
	source   string
}

func zeroFlowSamples(events []zeroevent.Envelope) ([]metering.FlowSample, error) {
	return zeroFlowSamplesWithMerge(events, true)
}
func zeroFlowSamplesWithMerge(events []zeroevent.Envelope, merge bool) ([]metering.FlowSample, error) {
	// Do not move a later sample backwards across another asset, even when
	// that other asset reported a DIFFERENT flow or only a stats observation.
	// The maps are bounded by this existing input window; no payload is copied.
	type sampleLineage struct {
		node uint64
		core string
	}
	tags := make(map[sampleLineage]string)
	mixed := make(map[sampleLineage]bool)
	for _, event := range events {
		key := sampleLineage{event.NodeID, strings.TrimSpace(event.CoreInstanceID)}
		if previous, ok := tags[key]; ok {
			mixed[key] = mixed[key] || previous != event.AssetTag
		} else {
			tags[key] = event.AssetTag
		}
	}
	result := make([]metering.FlowSample, 0, len(events))
	latest := make(map[zeroBufferedFlowKey]zeroSamplePosition)
	type reportKey struct {
		node uint64
		id   string
	}
	reports := make(map[reportKey]int)
	for _, event := range events {
		if event.Type == "flow.updated" {
			reports[reportKey{event.NodeID, strings.TrimSpace(event.ID)}]++
		}
	}
	for _, buffered := range events {
		if buffered.Type != "flow.updated" || buffered.NodeID == 0 {
			continue
		}
		event := zeroBufferedEnvelopeAsEvent(buffered)
		flow, err := parseZeroFlowProjection(event)
		if err != nil {
			return nil, err
		}
		if flow.PrincipalKey == "" {
			return nil, &zeroEventRequestError{message: "flow event has no attributable principal_key"}
		}
		if isMieruMigrationPrincipal(flow.PrincipalKey) {
			continue
		}
		sample := metering.FlowSample{NodeID: uint(buffered.NodeID), SourceID: event.SourceID, CoreInstanceID: event.CoreInstanceID, EventID: event.EventID, EventType: event.EventType, Sequence: event.Sequence, FlowID: flow.FlowID, PrincipalKey: flow.PrincipalKey, Revision: flow.Revision, BytesUp: flow.BytesUp, BytesDown: flow.BytesDown, OccurredAt: zeroEventTime(event, time.Now().UTC())}
		key := zeroBufferedFlowKey{buffered.NodeID, strings.TrimSpace(event.CoreInstanceID), flow.FlowID}
		minute := sample.OccurredAt.Unix() / 60
		position, exists := latest[key]
		safe := !mixed[sampleLineage{buffered.NodeID, key.CoreInstanceID}] && key.CoreInstanceID != "" && sample.Sequence > 0 && !buffered.OccurredAt.IsZero() && reports[reportKey{buffered.NodeID, event.EventID}] == 1
		// A tag transition is a merge boundary even if a prior tag returns.
		// Keep the last position under the ORIGINAL financial flow key so A/B/A
		// cannot merge backwards across B and reorder accounting checkpoints.
		if merge && exists && safe && position.assetTag == buffered.AssetTag {
			previous := &result[position.sample]
			if sample.PrincipalKey == previous.PrincipalKey && sample.Sequence > previous.Sequence && sample.BytesUp >= previous.BytesUp && sample.BytesDown >= previous.BytesDown && position.revision == sample.Revision && position.config == event.ConfigRevision && position.minute == minute && position.source == event.SourceID {
				points := previous.Checkpoints
				if len(points) == 0 {
					points = append(points, metering.FlowCheckpoint{Sequence: previous.Sequence, BytesUp: previous.BytesUp, BytesDown: previous.BytesDown, ReportID: previous.EventID})
				}
				sample.Checkpoints = append(points, metering.FlowCheckpoint{Sequence: sample.Sequence, BytesUp: sample.BytesUp, BytesDown: sample.BytesDown, ReportID: sample.EventID})
				*previous = sample
				continue
			}
		}
		result = append(result, sample)
		if safe {
			latest[key] = zeroSamplePosition{buffered.AssetTag, len(result) - 1, sample.Revision, event.ConfigRevision, minute, event.SourceID}
		} else {
			delete(latest, key)
		}
	}
	return result, nil
}
