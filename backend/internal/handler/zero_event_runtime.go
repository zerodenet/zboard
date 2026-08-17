package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	zeroEventConsumerBurstBatches          = 8
	zeroEventConsumerWarningBurstBatches   = 12
	zeroEventConsumerCompactBurstBatches   = 16
	zeroEventConsumerEmergencyBurstBatches = 32
	zeroEventConsumerMinimumInterval       = 100 * time.Millisecond
)

var zeroEventRuntimeRegistry sync.Map

type zeroEventRuntime struct {
	spool  zeroevent.EventSpool
	config zeroevent.ConsumerConfig
	cancel context.CancelFunc
	done   chan struct{}
}

type zeroEventNodeCursor struct {
	NodeID         uint      `gorm:"column:node_id;primaryKey"`
	CoreInstanceID string    `gorm:"column:core_instance_id"`
	Sequence       uint64    `gorm:"column:sequence"`
	ConfigRevision uint64    `gorm:"column:config_revision"`
	OccurredAt     time.Time `gorm:"column:occurred_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

func (zeroEventNodeCursor) TableName() string { return "zero_event_node_cursors" }

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
	go h.runZeroEventConsumer(ctx, runtime)
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
	runtime.cancel()
	<-runtime.done
	if err := runtime.spool.Close(); err != nil {
		return err
	}
	log.Printf("Zero event spool stopped")
	return nil
}

func (h *handlers) appendBufferedZeroEvent(ctx context.Context, node model.Node, event zeroEventEnvelope) (bool, error) {
	if event.EventType != "flow.updated" && event.EventType != "stats.sampled" {
		return false, nil
	}
	value, ok := zeroEventRuntimeRegistry.Load(h)
	if !ok {
		return false, nil
	}
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
	envelope := zeroevent.Envelope{
		ID:             strings.TrimSpace(event.EventID),
		NodeID:         uint64(node.ID),
		SourceID:       strings.TrimSpace(event.SourceID),
		PrincipalKey:   strings.TrimSpace(event.PrincipalKey),
		Type:           event.EventType,
		OccurredAt:     zeroEventTime(event, time.Now().UTC()),
		CoreInstanceID: strings.TrimSpace(event.CoreInstanceID),
		ConfigRevision: event.ConfigRevision,
		FlowID:         flowID,
		Sequence:       event.Sequence,
		Payload:        append(json.RawMessage(nil), event.Payload...),
	}
	if err := value.(*zeroEventRuntime).spool.Append(ctx, envelope); err != nil {
		return true, fmt.Errorf("persist Zero event %s: %w", event, err)
	}
	return true, nil
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

func (h *handlers) runZeroEventConsumer(ctx context.Context, runtime *zeroEventRuntime) {
	defer close(runtime.done)
	consume := func() {
		burst := zeroEventConsumerBurst(runtime.spool.Status())
		for index := 0; index < burst; index++ {
			count, err := h.consumeZeroEventBatch(ctx, runtime.spool, runtime.config.MaxBatch)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("Zero event projector failed: %v", err)
				}
				return
			}
			if count < runtime.config.MaxBatch {
				return
			}
		}
	}
	consume()
	timer := time.NewTimer(zeroEventConsumerInterval(runtime.config.CommitInterval, runtime.spool.Status()))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			consume()
			timer.Reset(zeroEventConsumerInterval(runtime.config.CommitInterval, runtime.spool.Status()))
		}
	}
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
	batch, err := spool.ReadBatch(ctx, limit)
	if err != nil {
		return 0, err
	}
	if len(batch.Events) == 0 {
		return 0, nil
	}
	if err := h.projectZeroNodeEvents(ctx, batch.Events); err != nil {
		return 0, err
	}
	if err := spool.Commit(ctx, batch.Next); err != nil {
		return 0, fmt.Errorf("commit Zero event checkpoint: %w", err)
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

	exhausted := make([]zeroFlowAccountingResult, 0)
	err := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, nodeID := range nodeIDs {
			if err := projectZeroNode(tx, nodeProjections[nodeID]); err != nil {
				return err
			}
		}
		for _, event := range flowEvents {
			result, err := h.projectBufferedZeroFlow(tx, event)
			if err != nil {
				return err
			}
			if result.Exhausted {
				exhausted = append(exhausted, result)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, result := range exhausted {
		h.scheduleNodeConfigPublish(result.NodeID, result.ProtocolEndpointID, 0)
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

func projectZeroNode(tx *gorm.DB, projection zeroNodeProjection) error {
	var cursor zeroEventNodeCursor
	cursorErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", projection.NodeID).First(&cursor).Error
	if cursorErr != nil && !errors.Is(cursorErr, gorm.ErrRecordNotFound) {
		return cursorErr
	}
	if cursorErr == nil && !zeroEnvelopeNewerThanCursor(projection.Latest, cursor) {
		return nil
	}

	lastSeen := projection.Latest.OccurredAt.UTC()
	if lastSeen.IsZero() {
		lastSeen = time.Now().UTC()
	}
	now := time.Now().UTC()
	online := !lastSeen.Add(nodeOnlineWindow).Before(now)
	updates := map[string]interface{}{
		"last_seen_at":           lastSeen,
		"connector_last_seen_at": lastSeen,
		"is_online":              online,
		"status":                 map[bool]int16{true: 1, false: 0}[online],
	}
	if projection.StatsEvent != nil && (cursorErr != nil || zeroEnvelopeNewerThanCursor(*projection.StatsEvent, cursor)) {
		updates["active_flows"] = projection.Stats.ActiveSessions
		updates["bytes_up"] = projection.Stats.BytesUp
		updates["bytes_down"] = projection.Stats.BytesDown
	}
	result := tx.Model(&model.Node{}).
		Where("id = ? AND is_enabled = ? AND node_credential_revoked_at IS NULL", projection.NodeID, true).
		Where("last_seen_at IS NULL OR last_seen_at <= ?", lastSeen).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}

	next := zeroEventNodeCursor{
		NodeID:         projection.NodeID,
		CoreInstanceID: strings.TrimSpace(projection.Latest.CoreInstanceID),
		Sequence:       projection.Latest.Sequence,
		ConfigRevision: projection.Latest.ConfigRevision,
		OccurredAt:     lastSeen,
		UpdatedAt:      now,
	}
	if errors.Is(cursorErr, gorm.ErrRecordNotFound) {
		return tx.Create(&next).Error
	}
	return tx.Model(&zeroEventNodeCursor{}).Where("node_id = ?", projection.NodeID).Updates(map[string]interface{}{
		"core_instance_id": next.CoreInstanceID,
		"sequence":         next.Sequence,
		"config_revision":  next.ConfigRevision,
		"occurred_at":      next.OccurredAt,
		"updated_at":       next.UpdatedAt,
	}).Error
}

func zeroEventNewer(left, right zeroevent.Envelope) bool {
	leftInstance := strings.TrimSpace(left.CoreInstanceID)
	rightInstance := strings.TrimSpace(right.CoreInstanceID)
	if leftInstance != "" && leftInstance == rightInstance && left.Sequence > 0 && right.Sequence > 0 {
		return left.Sequence > right.Sequence
	}
	if !left.OccurredAt.Equal(right.OccurredAt) {
		return left.OccurredAt.After(right.OccurredAt)
	}
	if left.ConfigRevision != right.ConfigRevision {
		return left.ConfigRevision > right.ConfigRevision
	}
	if left.Sequence != right.Sequence {
		return left.Sequence > right.Sequence
	}
	return left.ID > right.ID
}

func zeroEnvelopeNewerThanCursor(event zeroevent.Envelope, cursor zeroEventNodeCursor) bool {
	instanceID := strings.TrimSpace(event.CoreInstanceID)
	cursorInstanceID := strings.TrimSpace(cursor.CoreInstanceID)
	if instanceID != "" && instanceID == cursorInstanceID && event.Sequence > 0 && cursor.Sequence > 0 {
		return event.Sequence > cursor.Sequence
	}
	occurredAt := event.OccurredAt.UTC()
	if !occurredAt.Equal(cursor.OccurredAt.UTC()) {
		return occurredAt.After(cursor.OccurredAt.UTC())
	}
	if event.ConfigRevision != cursor.ConfigRevision {
		return event.ConfigRevision > cursor.ConfigRevision
	}
	return event.Sequence > cursor.Sequence
}
