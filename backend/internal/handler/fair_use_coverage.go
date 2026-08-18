package handler

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
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
	coreInstanceID := strings.TrimSpace(event.CoreInstanceID)
	if nodeID == 0 || coreInstanceID == "" || event.Sequence == 0 {
		return nil
	}
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	receivedAt = receivedAt.UTC()
	occurredAt := zeroEventTime(event, receivedAt).UTC()
	eventID := strings.TrimSpace(event.EventID)

	return h.db.Transaction(func(tx *gorm.DB) error {
		var current fairUseNodeCoverage
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", nodeID).First(&current).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(&fairUseNodeCoverage{
				NodeID:              nodeID,
				CoreInstanceID:      coreInstanceID,
				LastSequence:        event.Sequence,
				LastEventID:         eventID,
				ContinuousSinceAt:   receivedAt,
				LastReceivedAt:      receivedAt,
				LastEventOccurredAt: occurredAt,
				UpdatedAt:           receivedAt,
			}).Error
		}

		if current.CoreInstanceID != coreInstanceID {
			// A different Core instance is a new delivery generation. Ignore an
			// obviously late event from an older generation rather than allowing
			// it to reset the coverage cursor backwards.
			if !current.LastEventOccurredAt.IsZero() && occurredAt.Before(current.LastEventOccurredAt) && event.EventType != "engine.started" {
				return nil
			}
			return tx.Model(&fairUseNodeCoverage{}).Where("node_id = ?", nodeID).Updates(map[string]interface{}{
				"core_instance_id":       coreInstanceID,
				"last_sequence":          event.Sequence,
				"last_event_id":          eventID,
				"continuous_since_at":    receivedAt,
				"last_received_at":       receivedAt,
				"last_event_occurred_at": occurredAt,
				"last_gap_from_sequence": 0,
				"last_gap_to_sequence":   0,
				"last_gap_at":            nil,
				"updated_at":             receivedAt,
			}).Error
		}

		receiptInterrupted := !current.LastReceivedAt.IsZero() && receivedAt.Sub(current.LastReceivedAt) > fairUseCoverageFreshness
		if event.Sequence <= current.LastSequence {
			// Duplicate/replayed older deliveries do not heal a previously
			// detected sequence gap. A long receive-side silence still resets the
			// trustworthy continuous window so a replay burst after reconnect
			// cannot be scored as fresh subscriber behaviour.
			updates := map[string]interface{}{
				"last_received_at": receivedAt,
				"updated_at":       receivedAt,
			}
			if receiptInterrupted {
				updates["continuous_since_at"] = receivedAt
			}
			return tx.Model(&fairUseNodeCoverage{}).Where("node_id = ?", nodeID).Updates(updates).Error
		}

		updates := map[string]interface{}{
			"last_sequence":          event.Sequence,
			"last_event_id":          eventID,
			"last_received_at":       receivedAt,
			"last_event_occurred_at": occurredAt,
			"updated_at":             receivedAt,
		}
		if receiptInterrupted {
			updates["continuous_since_at"] = receivedAt
		}
		if event.Sequence > current.LastSequence+1 {
			missing := event.Sequence - current.LastSequence - 1
			gapAt := receivedAt
			updates["continuous_since_at"] = receivedAt
			updates["last_gap_from_sequence"] = current.LastSequence + 1
			updates["last_gap_to_sequence"] = event.Sequence - 1
			updates["last_gap_at"] = gapAt
			updates["gap_count"] = gorm.Expr("gap_count + ?", missing)
		}
		return tx.Model(&fairUseNodeCoverage{}).Where("node_id = ?", nodeID).Updates(updates).Error
	})
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
