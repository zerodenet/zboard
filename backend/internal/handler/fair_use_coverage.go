package handler

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const fairUseCoverageFreshness = 30 * time.Second

type fairUseNodeCoverage struct {
	NodeID              uint       `gorm:"column:node_id;primaryKey"`
	CoreInstanceID      string     `gorm:"column:core_instance_id"`
	LastSequence        uint64     `gorm:"column:last_sequence"`
	LastEventID         string     `gorm:"column:last_event_id"`
	ContinuousSinceAt   time.Time  `gorm:"column:continuous_since_at"`
	LastReceivedAt      time.Time  `gorm:"column:last_received_at"`
	LastEventOccurredAt time.Time  `gorm:"column:last_event_occurred_at"`
	LastGapFromSequence uint64     `gorm:"column:last_gap_from_sequence"`
	LastGapToSequence   uint64     `gorm:"column:last_gap_to_sequence"`
	LastGapAt           *time.Time `gorm:"column:last_gap_at"`
	GapCount            uint64     `gorm:"column:gap_count"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
}

func (fairUseNodeCoverage) TableName() string { return "fair_use_node_coverage" }

type fairUseCoverageNode struct {
	NodeID              uint       `json:"node_id"`
	State               string     `json:"state"`
	Reason              string     `json:"reason"`
	CoreInstanceID      string     `json:"core_instance_id,omitempty"`
	LastSequence        uint64     `json:"last_sequence,omitempty"`
	ContinuousSinceAt   *time.Time `json:"continuous_since_at,omitempty"`
	LastReceivedAt      *time.Time `json:"last_received_at,omitempty"`
	LastGapFromSequence uint64     `json:"last_gap_from_sequence,omitempty"`
	LastGapToSequence   uint64     `json:"last_gap_to_sequence,omitempty"`
	LastGapAt           *time.Time `json:"last_gap_at,omitempty"`
}

type fairUseCoverageSummary struct {
	State         string                `json:"state"`
	Reason        string                `json:"reason"`
	WindowSeconds int                   `json:"window_seconds"`
	RequiredNodes int                   `json:"required_nodes"`
	CompleteNodes int                   `json:"complete_nodes"`
	Nodes         []fairUseCoverageNode `json:"nodes"`
}

func (h *handlers) observeFairUseEventCoverage(nodeID uint, event zeroEventEnvelope, receivedAt time.Time) error {
	if nodeID == 0 || strings.TrimSpace(event.CoreInstanceID) == "" || event.Sequence == 0 {
		return nil
	}
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	receivedAt = receivedAt.UTC()
	return h.db.Transaction(func(tx *gorm.DB) error {
		var current fairUseNodeCoverage
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", nodeID).First(&current).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		exists := err == nil
		next, changed := advanceFairUseCoverage(current, exists, nodeID, event, receivedAt)
		if !changed {
			return nil
		}
		return persistFairUseCoverage(tx, next, exists)
	})
}

func advanceFairUseCoverage(current fairUseNodeCoverage, exists bool, nodeID uint, event zeroEventEnvelope, receivedAt time.Time) (fairUseNodeCoverage, bool) {
	coreInstanceID := strings.TrimSpace(event.CoreInstanceID)
	if nodeID == 0 || coreInstanceID == "" || event.Sequence == 0 {
		return current, false
	}
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	receivedAt = receivedAt.UTC()
	occurredAt := zeroEventTime(event, receivedAt).UTC()
	eventID := strings.TrimSpace(event.EventID)
	if !exists {
		return fairUseNodeCoverage{
			NodeID: nodeID, CoreInstanceID: coreInstanceID, LastSequence: event.Sequence,
			LastEventID: eventID, ContinuousSinceAt: receivedAt, LastReceivedAt: receivedAt,
			LastEventOccurredAt: occurredAt, UpdatedAt: receivedAt,
		}, true
	}

	if current.CoreInstanceID != coreInstanceID {
		if !current.LastEventOccurredAt.IsZero() && occurredAt.Before(current.LastEventOccurredAt) && event.EventType != "engine.started" {
			return current, false
		}
		current.CoreInstanceID = coreInstanceID
		current.LastSequence = event.Sequence
		current.LastEventID = eventID
		current.ContinuousSinceAt = receivedAt
		current.LastReceivedAt = receivedAt
		current.LastEventOccurredAt = occurredAt
		current.LastGapFromSequence = 0
		current.LastGapToSequence = 0
		current.LastGapAt = nil
		current.UpdatedAt = receivedAt
		return current, true
	}

	receiptInterrupted := !current.LastReceivedAt.IsZero() && receivedAt.Sub(current.LastReceivedAt) > fairUseCoverageFreshness
	if event.Sequence <= current.LastSequence {
		current.LastReceivedAt = receivedAt
		current.UpdatedAt = receivedAt
		if receiptInterrupted {
			current.ContinuousSinceAt = receivedAt
		}
		return current, true
	}

	previousSequence := current.LastSequence
	current.LastSequence = event.Sequence
	current.LastEventID = eventID
	current.LastReceivedAt = receivedAt
	current.LastEventOccurredAt = occurredAt
	current.UpdatedAt = receivedAt
	if receiptInterrupted {
		current.ContinuousSinceAt = receivedAt
	}
	if event.Sequence > previousSequence+1 {
		missing := event.Sequence - previousSequence - 1
		gapAt := receivedAt
		current.ContinuousSinceAt = receivedAt
		current.LastGapFromSequence = previousSequence + 1
		current.LastGapToSequence = event.Sequence - 1
		current.LastGapAt = &gapAt
		current.GapCount += missing
	}
	return current, true
}

func persistFairUseCoverage(tx *gorm.DB, row fairUseNodeCoverage, exists bool) error {
	if !exists {
		return tx.Create(&row).Error
	}
	return tx.Model(&fairUseNodeCoverage{}).Where("node_id = ?", row.NodeID).Updates(map[string]interface{}{
		"core_instance_id": row.CoreInstanceID, "last_sequence": row.LastSequence,
		"last_event_id": row.LastEventID, "continuous_since_at": row.ContinuousSinceAt,
		"last_received_at": row.LastReceivedAt, "last_event_occurred_at": row.LastEventOccurredAt,
		"last_gap_from_sequence": row.LastGapFromSequence, "last_gap_to_sequence": row.LastGapToSequence,
		"last_gap_at": row.LastGapAt, "gap_count": row.GapCount, "updated_at": row.UpdatedAt,
	}).Error
}

func (h *handlers) projectFairUseCoverageBatch(tx *gorm.DB, events []zeroevent.Envelope) error {
	nodeSet := make(map[uint]struct{})
	for _, buffered := range events {
		if buffered.NodeID > 0 && strings.TrimSpace(buffered.CoreInstanceID) != "" && buffered.Sequence > 0 {
			nodeSet[uint(buffered.NodeID)] = struct{}{}
		}
	}
	if len(nodeSet) == 0 {
		return nil
	}
	nodeIDs := make([]uint, 0, len(nodeSet))
	for nodeID := range nodeSet {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Slice(nodeIDs, func(i, j int) bool { return nodeIDs[i] < nodeIDs[j] })

	var rows []fairUseNodeCoverage
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id IN ?", nodeIDs).Order("node_id asc").Find(&rows).Error; err != nil {
		return err
	}
	byNode := make(map[uint]fairUseNodeCoverage, len(rows))
	existed := make(map[uint]bool, len(rows))
	for _, row := range rows {
		byNode[row.NodeID] = row
		existed[row.NodeID] = true
	}
	changed := make(map[uint]bool, len(nodeIDs))
	batchReceivedAt := time.Now().UTC()
	for _, buffered := range events {
		nodeID := uint(buffered.NodeID)
		if _, tracked := nodeSet[nodeID]; !tracked {
			continue
		}
		receivedAt := buffered.ReceivedAt
		if receivedAt.IsZero() {
			receivedAt = batchReceivedAt
		}
		next, didChange := advanceFairUseCoverage(byNode[nodeID], existed[nodeID] || changed[nodeID], nodeID, zeroBufferedEnvelopeAsEvent(buffered), receivedAt)
		if didChange {
			byNode[nodeID] = next
			changed[nodeID] = true
		}
	}
	for _, nodeID := range nodeIDs {
		if changed[nodeID] {
			if err := persistFairUseCoverage(tx, byNode[nodeID], existed[nodeID]); err != nil {
				return err
			}
		}
	}
	return nil
}

func classifyFairUseCoverage(row fairUseNodeCoverage, exists bool, cutoff, freshCutoff time.Time) (string, string) {
	if !exists || strings.TrimSpace(row.CoreInstanceID) == "" || row.LastSequence == 0 {
		return "unknown", "no_sequence_coverage"
	}
	if row.LastReceivedAt.Before(freshCutoff) {
		return "incomplete", "stale_connector_coverage"
	}
	if row.ContinuousSinceAt.After(cutoff) {
		if row.LastGapAt != nil && !row.LastGapAt.Before(cutoff) {
			return "incomplete", "sequence_gap_in_window"
		}
		return "unknown", "coverage_warming"
	}
	return "complete", "continuous_sequence_coverage"
}

func (h *handlers) fairUseCoverageForSubscription(subscriptionID uint, windowSeconds int, now time.Time) (fairUseCoverageSummary, error) {
	summary := fairUseCoverageSummary{
		State:         "unknown",
		Reason:        "no_active_credentials",
		WindowSeconds: windowSeconds,
		Nodes:         make([]fairUseCoverageNode, 0),
	}
	if subscriptionID == 0 {
		return summary, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	var nodeIDs []uint
	if err := h.db.Model(&model.ProtocolCredential{}).
		Joins("JOIN nodes ON nodes.id = protocol_credentials.node_id AND nodes.is_enabled = ?", true).
		Joins("JOIN protocol_endpoints ON protocol_endpoints.id = protocol_credentials.protocol_endpoint_id AND protocol_endpoints.is_active = ?", true).
		Where("protocol_credentials.subscription_id = ? AND protocol_credentials.status = ? AND protocol_credentials.revoked_at IS NULL AND protocol_credentials.expires_at > ?", subscriptionID, "active", now).
		Distinct("protocol_credentials.node_id").Pluck("protocol_credentials.node_id", &nodeIDs).Error; err != nil {
		return summary, err
	}
	sort.Slice(nodeIDs, func(i, j int) bool { return nodeIDs[i] < nodeIDs[j] })
	summary.RequiredNodes = len(nodeIDs)
	if len(nodeIDs) == 0 {
		return summary, nil
	}

	var rows []fairUseNodeCoverage
	if err := h.db.Where("node_id IN ?", nodeIDs).Find(&rows).Error; err != nil {
		return summary, err
	}
	byNode := make(map[uint]fairUseNodeCoverage, len(rows))
	for _, row := range rows {
		byNode[row.NodeID] = row
	}

	cutoff := now.Add(-time.Duration(windowSeconds) * time.Second)
	freshCutoff := now.Add(-fairUseCoverageFreshness)
	hasIncomplete := false
	hasUnknown := false
	for _, nodeID := range nodeIDs {
		row, ok := byNode[nodeID]
		state, reason := classifyFairUseCoverage(row, ok, cutoff, freshCutoff)
		detail := fairUseCoverageNode{
			NodeID: nodeID,
			State:  state,
			Reason: reason,
		}
		if ok {
			detail.CoreInstanceID = row.CoreInstanceID
			detail.LastSequence = row.LastSequence
			detail.LastGapFromSequence = row.LastGapFromSequence
			detail.LastGapToSequence = row.LastGapToSequence
			detail.LastGapAt = row.LastGapAt
			continuous := row.ContinuousSinceAt.UTC()
			lastReceived := row.LastReceivedAt.UTC()
			detail.ContinuousSinceAt = &continuous
			detail.LastReceivedAt = &lastReceived
		}
		switch state {
		case "incomplete":
			hasIncomplete = true
		case "unknown":
			hasUnknown = true
		case "complete":
			summary.CompleteNodes++
		}
		summary.Nodes = append(summary.Nodes, detail)
	}

	switch {
	case hasIncomplete:
		summary.State = "incomplete"
		summary.Reason = "one_or_more_nodes_incomplete"
	case hasUnknown:
		summary.State = "unknown"
		summary.Reason = "one_or_more_nodes_unknown"
	default:
		summary.State = "complete"
		summary.Reason = "all_active_credential_nodes_continuous"
	}
	return summary, nil
}
