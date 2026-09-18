package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/application"
	capabilityjobs "github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

const (
	zeroEventConsumerBurstBatches          = 8
	zeroEventConsumerWarningBurstBatches   = 12
	zeroEventConsumerCompactBurstBatches   = 16
	zeroEventConsumerEmergencyBurstBatches = 32
	zeroEventConsumerMinimumInterval       = 100 * time.Millisecond
	zeroEventSQLiteBatchLimit              = 32
	zeroConnectorReceiptPersistInterval    = 30 * time.Second
)

var zeroEventRuntimeRegistry sync.Map

type zeroEventRuntime struct {
	spool       zeroevent.EventSpool
	config      zeroevent.ConsumerConfig
	cancel      context.CancelFunc
	done        chan struct{}
	lastReceipt sync.Map
	metrics     zeroEventConsumerMetrics
}

type zeroEventNodeCursor = networkstore.ObservationCursor

type zeroNodeProjection struct {
	NodeID     uint
	Latest     zeroevent.Envelope
	StatsEvent *zeroevent.Envelope
	Stats      zeroStatsProjection
}

func (h *handlers) ConfigureZeroEventSpool(cfg zeroevent.Config) error {
	h.StartCredentialExpiryWorker()
	if err := h.ReconcileHistoryRetentionDefaults(); err != nil {
		h.CloseCredentialExpiryWorker()
		return err
	}
	h.StartHistoryRetentionWorker()
	if !cfg.Enabled {
		return nil
	}
	spool, err := zeroevent.NewFileSpool(cfg)
	if err != nil {
		h.CloseHistoryRetentionWorker()
		h.CloseCredentialExpiryWorker()
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := spool.Start(ctx); err != nil {
		cancel()
		h.CloseHistoryRetentionWorker()
		h.CloseCredentialExpiryWorker()
		return err
	}
	runtime := &zeroEventRuntime{
		spool:  spool,
		config: cfg.Consumer,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	if _, loaded := zeroEventRuntimeRegistry.LoadOrStore(h, runtime); loaded {
		cancel()
		_ = spool.Close()
		h.CloseHistoryRetentionWorker()
		h.CloseCredentialExpiryWorker()
		return errors.New("Zero event spool is already configured")
	}
	h.backgroundJobs().observations.Register("event_consumer", jobNames["event_consumer"], "queue", cfg.Consumer.CommitInterval, 1)
	h.startScheduledJob("event_consumer", cfg.Consumer.CommitInterval, func(ctx context.Context) error { return h.consumeZeroEventCycle(ctx, runtime) })
	log.Printf("Zero event spool started: driver=%s directory=%s commit_interval=%s max_batch=%d", cfg.Driver, cfg.Directory, cfg.Consumer.CommitInterval, cfg.Consumer.MaxBatch)
	return nil
}

func (h *handlers) CloseZeroEventSpool() error {
	h.CloseHistoryRetentionWorker()
	h.CloseCredentialExpiryWorker()
	value, ok := zeroEventRuntimeRegistry.LoadAndDelete(h)
	if !ok {
		return nil
	}
	runtime := value.(*zeroEventRuntime)
	h.closeScheduledJob("event_consumer")
	runtime.cancel()
	close(runtime.done)
	if err := runtime.spool.Close(); err != nil {
		return err
	}
	log.Printf("Zero event spool stopped")
	return nil
}

func (h *handlers) appendBufferedZeroEvent(ctx context.Context, node network.EventNode, event zeroEventEnvelope) (bool, error) {
	if event.EventType != "flow.updated" && event.EventType != "stats.sampled" {
		return false, nil
	}
	value, ok := zeroEventRuntimeRegistry.Load(h)
	if !ok {
		return false, nil
	}
	runtime := value.(*zeroEventRuntime)
	flowID := ""
	if event.EventType == "stats.sampled" {
		if _, err := parseZeroStatsProjection(event.Payload); err != nil {
			return true, err
		}
	} else {
		flow, err := parseZeroFlowProjection(event)
		if err != nil {
			return true, err
		}
		if flow.PrincipalKey == "" {
			return true, errors.New("flow event has no attributable principal_key")
		}
		flowID = flow.FlowID
	}
	receivedAt := time.Now().UTC()
	if state := zeroEventStateFromContext(ctx); state != nil && !state.receivedAt.IsZero() {
		receivedAt = state.receivedAt.UTC()
	}
	envelope := zeroevent.Envelope{
		ID:             strings.TrimSpace(event.EventID),
		NodeID:         uint64(node.ID),
		SourceID:       strings.TrimSpace(event.SourceID),
		PrincipalKey:   strings.TrimSpace(event.PrincipalKey),
		Type:           event.EventType,
		OccurredAt:     zeroEventTime(event, time.Now().UTC()),
		ReceivedAt:     receivedAt,
		CoreInstanceID: strings.TrimSpace(event.CoreInstanceID),
		ConfigRevision: event.ConfigRevision,
		FlowID:         flowID,
		Sequence:       event.Sequence,
		Payload:        append(json.RawMessage(nil), event.Payload...),
	}
	if err := runtime.spool.Append(ctx, envelope); err != nil {
		return true, fmt.Errorf("persist Zero event %s: %w", event, err)
	}
	if err := h.recordBufferedZeroConnectorReceipt(ctx, node, runtime); err != nil {
		// The durable event has already been accepted. Liveness projection is
		// deliberately best-effort here so a transient metadata write cannot make
		// Core retry an event that is safely stored in the spool.
		log.Printf("Zero connector receipt projection failed for node %d: %v", node.ID, err)
	}
	return true, nil
}

func (h *handlers) recordBufferedZeroConnectorReceipt(ctx context.Context, node network.EventNode, runtime *zeroEventRuntime) error {
	now := time.Now().UTC()
	if previous, ok := runtime.lastReceipt.Load(node.ID); ok {
		if last, ok := previous.(time.Time); ok && now.Sub(last) < zeroConnectorReceiptPersistInterval {
			return nil
		}
	}
	if err := h.services.NodeActivity.Record(ctx, node.ID, node.Credential, network.NodeActivityUpdate{At: now, Online: true, ConnectorSeen: true}); err != nil {
		if !errors.Is(err, network.ErrNodeActivityCredential) {
			return err
		}
		return errors.New("Zero event credential is no longer active")
	}
	runtime.lastReceipt.Store(node.ID, now)
	return nil
}

func zeroBufferedFlowID(payload json.RawMessage) string {
	var value map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil || value == nil {
		return ""
	}
	record := value
	if nested, ok := value["record"].(map[string]interface{}); ok {
		record = nested
	}
	flowID := strings.TrimSpace(stringValue(record["flow_id"]))
	if flowID == "" {
		flowID = strings.TrimSpace(stringValue(value["flow_id"]))
	}
	return flowID
}

func (h *handlers) consumeZeroEventCycle(ctx context.Context, runtime *zeroEventRuntime) error {
	limit := zeroEventConsumerBatchLimit(runtime.config.MaxBatch, h.services.TrafficReadsUseSQLite())
	burst := zeroEventConsumerBurst(runtime.spool.Status())
	for index := 0; index < burst; index++ {
		count, err := h.consumeZeroEventBatchMeasured(ctx, runtime.spool, limit, &runtime.metrics)
		if err != nil {
			runtime.metrics.failures.Add(1)
			return err
		}
		if count < limit {
			return nil
		}
	}
	return capabilityjobs.ErrContinue
}

// SQLite shares a single connection with live authorization and management
// reads. Keep transactions short; each committed batch retains its own durable
// checkpoint. MySQL keeps the configured batch size.
func zeroEventConsumerBatchLimit(configured int, sqlite bool) int {
	if sqlite && configured > zeroEventSQLiteBatchLimit {
		return zeroEventSQLiteBatchLimit
	}
	return configured
}

func zeroEventConsumerNextInterval(base time.Duration, status zeroevent.Status, fullBurst bool) time.Duration {
	if fullBurst {
		// A full burst may leave work queued. Do not impose another idle flush
		// interval on that backlog after reducing the transaction size.
		return zeroEventConsumerMinimumInterval
	}
	return zeroEventConsumerInterval(base, status)
}

func zeroEventConsumerBurst(status zeroevent.Status) int {
	switch {
	case status.Emergency:
		return zeroEventConsumerEmergencyBurstBatches
	case status.Compact:
		return zeroEventConsumerCompactBurstBatches
	case status.Warning:
		return zeroEventConsumerWarningBurstBatches
	default:
		return zeroEventConsumerBurstBatches
	}
}

func zeroEventConsumerInterval(base time.Duration, status zeroevent.Status) time.Duration {
	interval := base
	switch {
	case status.Emergency:
		interval = base / 10
	case status.Compact:
		interval = base / 4
	case status.Warning:
		interval = base / 2
	}
	if interval < zeroEventConsumerMinimumInterval {
		return zeroEventConsumerMinimumInterval
	}
	return interval
}

func (h *handlers) consumeZeroEventBatch(ctx context.Context, spool zeroevent.EventSpool, limit int) (int, error) {
	return h.consumeZeroEventBatchMeasured(ctx, spool, limit, nil)
}

func (h *handlers) consumeZeroEventBatchMeasured(ctx context.Context, spool zeroevent.EventSpool, limit int, metrics *zeroEventConsumerMetrics) (int, error) {
	started := time.Now()
	batch, err := spool.ReadBatch(ctx, limit)
	if err != nil {
		return 0, err
	}
	if len(batch.Events) == 0 {
		return 0, nil
	}
	finishObservation := h.observeJob("event_consumer", "queue", 0, 1)
	if err := h.projectZeroNodeEvents(ctx, batch.Events); err != nil {
		finishObservation(err)
		return 0, err
	}
	if err := spool.Commit(ctx, batch.Next); err != nil {
		finishObservation(err)
		return 0, fmt.Errorf("commit Zero event checkpoint: %w", err)
	}
	finishObservation(nil)
	if metrics != nil {
		metrics.committed(batch.Events, time.Since(started), time.Now().UTC())
	}
	return len(batch.Events), nil
}

func (h *handlers) projectZeroNodeEvents(ctx context.Context, events []zeroevent.Envelope) error {
	nodeProjections := aggregateZeroNodeEvents(events)
	flowEvents := aggregateZeroFlowEvents(events)
	if len(nodeProjections) == 0 && len(flowEvents) == 0 {
		return nil
	}
	nodeIDs := make([]uint, 0, len(nodeProjections))
	for nodeID := range nodeProjections {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Slice(nodeIDs, func(i, j int) bool { return nodeIDs[i] < nodeIDs[j] })

	samples := make([]metering.FlowSample, 0, len(flowEvents))
	for _, buffered := range flowEvents {
		event := zeroBufferedEnvelopeAsEvent(buffered)
		flow, err := parseZeroFlowProjection(event)
		if err != nil {
			return err
		}
		if flow.PrincipalKey == "" {
			return errors.New("flow event has no attributable principal_key")
		}
		if isMieruMigrationPrincipal(flow.PrincipalKey) {
			continue
		}
		samples = append(samples, metering.FlowSample{NodeID: uint(buffered.NodeID), SourceID: event.SourceID, CoreInstanceID: event.CoreInstanceID, EventID: event.EventID, EventType: event.EventType, Sequence: event.Sequence, FlowID: flow.FlowID, PrincipalKey: flow.PrincipalKey, Revision: flow.Revision, BytesUp: flow.BytesUp, BytesDown: flow.BytesDown, OccurredAt: zeroEventTime(event, time.Now().UTC())})
	}
	observations := make([]network.NodeObservation, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		observations = append(observations, zeroNodeObservation(nodeProjections[nodeID]))
	}
	coverage := make([]metering.NodeCoverageEvent, 0, len(events))
	now := time.Now().UTC()
	for _, event := range events {
		receivedAt := event.ReceivedAt
		if receivedAt.IsZero() {
			receivedAt = now
		}
		coverage = append(coverage, coverageInput(uint(event.NodeID), zeroBufferedEnvelopeAsEvent(event), receivedAt))
	}
	result, err := h.services.ProjectBufferedEvents(ctx, application.BufferedEventProjection{Nodes: observations, Flows: samples, Coverage: coverage}, h.credentialCipher)
	if err != nil {
		return err
	}
	// Coverage is observability state rather than accounting state. Fold it in
	// one transaction per spool batch, but keep it fail-open so an auxiliary
	// write can never stop traffic settlement or checkpoint progress.
	if result.CoverageErr != nil && ctx.Err() == nil {
		log.Printf("fair use buffered coverage projection failed: %v", result.CoverageErr)
	}
	if len(result.Exhausted) > 0 {
	}
	return nil
}

func aggregateZeroNodeEvents(events []zeroevent.Envelope) map[uint]zeroNodeProjection {
	result := make(map[uint]zeroNodeProjection)
	for _, event := range events {
		if event.NodeID == 0 || (event.Type != "flow.updated" && event.Type != "stats.sampled") {
			continue
		}
		nodeID := uint(event.NodeID)
		projection, exists := result[nodeID]
		if !exists || zeroEventNewer(event, projection.Latest) {
			projection.NodeID = nodeID
			projection.Latest = event
		}
		if event.Type == "stats.sampled" && (projection.StatsEvent == nil || zeroEventNewer(event, *projection.StatsEvent)) {
			stats, err := parseZeroStatsProjection(event.Payload)
			if err == nil {
				copyEvent := event
				projection.StatsEvent = &copyEvent
				projection.Stats = stats
			}
		}
		result[nodeID] = projection
	}
	return result
}

func networkEventPosition(event zeroevent.Envelope) network.EventPosition {
	return network.EventPosition{ID: event.ID, CoreInstanceID: event.CoreInstanceID, Sequence: event.Sequence, ConfigRevision: event.ConfigRevision, OccurredAt: event.OccurredAt}
}
func zeroNodeObservation(projection zeroNodeProjection) network.NodeObservation {
	input := network.NodeObservation{NodeID: projection.NodeID, Latest: networkEventPosition(projection.Latest), Stats: network.NodeStats(projection.Stats)}
	if projection.StatsEvent != nil {
		stats := networkEventPosition(*projection.StatsEvent)
		input.StatsEvent = &stats
	}
	return input
}
func zeroEventNewer(left, right zeroevent.Envelope) bool {
	return network.EventNewer(networkEventPosition(left), networkEventPosition(right))
}
func zeroEnvelopeNewerThanCursor(event zeroevent.Envelope, cursor zeroEventNodeCursor) bool {
	return network.EventNewerThanPosition(networkEventPosition(event), network.EventPosition{CoreInstanceID: cursor.CoreInstanceID, Sequence: cursor.Sequence, ConfigRevision: cursor.ConfigRevision, OccurredAt: cursor.OccurredAt})
}
