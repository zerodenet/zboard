package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	capabilityjobs "github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"log"
	"net/http"
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
	zeroEventConsumerCycleBudget           = 200 * time.Millisecond
	zeroEventSQLiteBatchLimit              = 32
	zeroEventMySQLBatchLimit               = 128
	zeroConnectorReceiptPersistInterval    = 30 * time.Second
)

var zeroEventRuntimeRegistry sync.Map

type zeroEventRuntime struct {
	spool       zeroevent.EventSpool
	config      zeroevent.ConsumerConfig
	cancel      context.CancelFunc
	done        chan struct{}
	lastReceipt sync.Map
	receipts    sync.Map
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
		h.detachScheduledJob("credential_expiry")
		return err
	}
	h.StartHistoryRetentionWorker()
	if !cfg.Enabled {
		return nil
	}
	spool, err := zeroevent.NewFileSpool(cfg)
	if err != nil {
		h.detachScheduledJob("history_retention")
		h.detachScheduledJob("credential_expiry")
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := spool.Start(ctx); err != nil {
		cancel()
		h.detachScheduledJob("history_retention")
		h.detachScheduledJob("credential_expiry")
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
		h.detachScheduledJob("history_retention")
		h.detachScheduledJob("credential_expiry")
		return errors.New("Zero event spool is already configured")
	}
	h.backgroundJobs().observations.Register("event_consumer", jobNames["event_consumer"], "queue", cfg.Consumer.CommitInterval, 1)
	h.startScheduledJob("event_consumer", cfg.Consumer.CommitInterval, func(ctx context.Context) error { return h.consumeZeroEventCycle(ctx, runtime) })
	log.Printf("Zero event spool started: driver=%s directory=%s commit_interval=%s max_batch=%d", cfg.Driver, cfg.Directory, cfg.Consumer.CommitInterval, cfg.Consumer.MaxBatch)
	return nil
}

func (h *handlers) CloseZeroEventSpool() error {
	// File-spool configuration and ownership are local to this process. Closing
	// it or failing startup must never revoke another process's durable tasks.
	h.detachScheduledJob("history_retention")
	h.detachScheduledJob("credential_expiry")
	value, ok := zeroEventRuntimeRegistry.LoadAndDelete(h)
	if !ok {
		return nil
	}
	runtime := value.(*zeroEventRuntime)
	h.detachScheduledJob("event_consumer")
	runtime.cancel()
	close(runtime.done)
	if err := runtime.spool.Close(); err != nil {
		return err
	}
	log.Printf("Zero event spool stopped")
	return nil
}

// Warnings and config notifications only feed node presence/coverage today.
// Keep them off the synchronous database path even during a warning storm.
func isZeroBufferedEvent(kind string) bool {
	switch kind {
	case "flow.updated", "stats.sampled", "engine.warning", "config.changed":
		return true
	}
	return false
}

func (h *handlers) appendBufferedZeroEvent(ctx context.Context, node network.EventNode, event zeroEventEnvelope) (bool, error) {
	if !isZeroBufferedEvent(event.EventType) {
		return false, nil
	}
	value, ok := zeroEventRuntimeRegistry.Load(h)
	if !ok {
		return false, nil
	}
	runtime := value.(*zeroEventRuntime)
	flowID := ""
	principal := strings.TrimSpace(event.PrincipalKey)
	payload := json.RawMessage(`{}`)
	if event.EventType == "stats.sampled" {
		stats, err := parseZeroStatsProjection(event.Payload)
		if err != nil {
			return true, &zeroEventRequestError{status: http.StatusBadRequest, message: err.Error()}
		}
		payload, err = json.Marshal(map[string]uint64{"active_sessions": stats.ActiveSessions, "bytes_up": stats.BytesUp, "bytes_down": stats.BytesDown})
		if err != nil {
			return true, &zeroEventRequestError{status: http.StatusBadRequest, message: err.Error()}
		}
	} else if event.EventType == "flow.updated" {
		flow, err := parseZeroFlowProjection(event)
		if err != nil {
			return true, &zeroEventRequestError{status: http.StatusBadRequest, message: err.Error()}
		}
		if flow.PrincipalKey == "" {
			return true, &zeroEventRequestError{status: http.StatusBadRequest, message: "flow event has no attributable principal_key"}
		}
		flowID = flow.FlowID
		principal = flow.PrincipalKey
		payload, err = json.Marshal(map[string]interface{}{"flow_id": flow.FlowID, "revision": flow.Revision, "traffic": map[string]int64{"bytes_up": flow.BytesUp, "bytes_down": flow.BytesDown}})
		if err != nil {
			return true, &zeroEventRequestError{status: http.StatusBadRequest, message: err.Error()}
		}
	}
	receivedAt := time.Now().UTC()
	if state := zeroEventStateFromContext(ctx); state != nil && !state.receivedAt.IsZero() {
		receivedAt = state.receivedAt.UTC()
	}
	envelope := zeroevent.Envelope{
		ID:             strings.TrimSpace(event.EventID),
		NodeID:         uint64(node.ID),
		SourceID:       strings.TrimSpace(event.SourceID),
		PrincipalKey:   principal,
		Type:           event.EventType,
		OccurredAt:     zeroEventTime(event, time.Now().UTC()),
		ReceivedAt:     receivedAt,
		CoreInstanceID: strings.TrimSpace(event.CoreInstanceID),
		ConfigRevision: event.ConfigRevision,
		FlowID:         flowID,
		Sequence:       event.Sequence,
		Payload:        payload,
	}
	if err := runtime.spool.Append(ctx, envelope); err != nil {
		return true, fmt.Errorf("persist Zero event %q: %w", event.EventID, err)
	}
	h.recordBufferedZeroConnectorReceipt(node, receivedAt, runtime)
	return true, nil
}

func (h *handlers) recordBufferedZeroConnectorReceipt(node network.EventNode, at time.Time, runtime *zeroEventRuntime) {
	// The event is already durable. Enqueue only; database writes belong to the
	// consumer so a locked node or exhausted pool never delays its HTTP receipt.
	runtime.enqueueReceipt(bufferedConnectorReceipt{node: node, at: at})
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
	started := time.Now()
	h.flushBufferedConnectorReceipts(ctx, runtime)
	limit := min(runtime.config.MaxBatch, zeroEventReadWindowLimit)
	burst := zeroEventConsumerBurst(runtime.spool.Status())
	for index := 0; index < burst; index++ {
		count, err := h.consumeZeroEventBatchMeasured(ctx, runtime.spool, limit, &runtime.metrics)
		if err != nil {
			runtime.metrics.failures.Add(1)
			return err
		}
		h.flushBufferedConnectorReceipts(ctx, runtime)
		if count == 0 {
			return nil
		}
		if count < limit {
			// A byte-limited window may still have a backlog even though its
			// record count is below the configured limit.
			if runtime.spool.Status().PendingEvents > 0 {
				return capabilityjobs.ErrContinue
			}
			return nil
		}
		// Yield between windows so an event flood cannot monopolize a job worker.
		// The budget is soft: the current bounded window completes first.
		if time.Since(started) >= zeroEventConsumerCycleBudget {
			return capabilityjobs.ErrContinue
		}
	}
	return capabilityjobs.ErrContinue
}

// SQLite shares a single connection with live authorization and management
// reads. Keep transactions short inside a separately bounded read window. The
// durable checkpoint advances only after every authority chunk succeeds.
func zeroEventConsumerBatchLimit(configured int, sqlite bool) int {
	if sqlite && configured > zeroEventSQLiteBatchLimit {
		return zeroEventSQLiteBatchLimit
	}
	if !sqlite && configured > zeroEventMySQLBatchLimit {
		return zeroEventMySQLBatchLimit
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
	projected, err := h.projectZeroNodeEventsBounded(ctx, batch.Events)
	if errors.Is(err, metering.ErrFlowWindowNeedsIndividualReports) {
		projected, err = h.projectZeroNodeEventsWithMerge(ctx, batch.Events, false)
	}
	if err != nil {
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
		metrics.projectedFlows.Add(uint64(projected))
		for _, event := range batch.Events {
			if event.Type == "flow.updated" {
				metrics.flowUpdates.Add(1)
			}
		}
	}
	return len(batch.Events), nil
}

func (h *handlers) projectZeroNodeEvents(ctx context.Context, events []zeroevent.Envelope) error {
	_, err := h.projectZeroNodeEventsBounded(ctx, events)
	if errors.Is(err, metering.ErrFlowWindowNeedsIndividualReports) {
		_, err = h.projectZeroNodeEventsWithMerge(ctx, events, false)
	}
	return err
}

func aggregateZeroNodeEvents(events []zeroevent.Envelope) map[uint]zeroNodeProjection {
	result := make(map[uint]zeroNodeProjection)
	for _, event := range events {
		if event.NodeID == 0 || !isZeroBufferedEvent(event.Type) {
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
