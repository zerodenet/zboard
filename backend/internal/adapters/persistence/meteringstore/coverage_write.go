package meteringstore

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sort"
	"strings"
	"time"
)

type CoverageWriter struct{ DB *gorm.DB }

func (s CoverageWriter) Observe(ctx context.Context, event metering.NodeCoverageEvent) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return ProjectCoverageBatch(tx, []metering.NodeCoverageEvent{event}) })
}
func persistCoverage(tx *gorm.DB, row CoverageRecord, exists bool) error {
	if !exists {
		return tx.Create(&row).Error
	}
	return tx.Model(&CoverageRecord{}).Where("node_id = ?", row.NodeID).Updates(map[string]interface{}{
		"core_instance_id": row.CoreInstanceID, "last_sequence": row.LastSequence,
		"last_event_id": row.LastEventID, "continuous_since_at": row.ContinuousSinceAt,
		"last_received_at": row.LastReceivedAt, "last_event_occurred_at": row.LastEventOccurredAt,
		"last_gap_from_sequence": row.LastGapFromSequence, "last_gap_to_sequence": row.LastGapToSequence,
		"last_gap_at": row.LastGapAt, "gap_count": row.GapCount, "updated_at": row.UpdatedAt,
	}).Error
}

func ProjectCoverageBatch(tx *gorm.DB, events []metering.NodeCoverageEvent) error {
	nodeSet := make(map[uint]struct{})
	for _, buffered := range events {
		if buffered.NodeID > 0 && strings.TrimSpace(buffered.Event.CoreInstanceID) != "" && buffered.Event.Sequence > 0 {
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

	var rows []CoverageRecord
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id IN ?", nodeIDs).Order("node_id asc").Find(&rows).Error; err != nil {
		return err
	}
	byNode := make(map[uint]CoverageRecord, len(rows))
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
		next, didChange := metering.AdvanceCoverage(metering.CoverageFact(byNode[nodeID]), existed[nodeID] || changed[nodeID], nodeID, buffered.Event, receivedAt)
		if didChange {
			byNode[nodeID] = CoverageRecord(next)
			changed[nodeID] = true
		}
	}
	for _, nodeID := range nodeIDs {
		if changed[nodeID] {
			if err := persistCoverage(tx, byNode[nodeID], existed[nodeID]); err != nil {
				return err
			}
		}
	}
	return nil
}
