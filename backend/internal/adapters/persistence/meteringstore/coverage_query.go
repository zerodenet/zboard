package meteringstore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"sort"
	"time"
)

func loadCoverage(db *gorm.DB, subscriptionID uint, windowSeconds int, now time.Time) (metering.CoverageSummary, error) {
	summary := metering.CoverageSummary{
		State:         "unknown",
		Reason:        "no_active_credentials",
		WindowSeconds: windowSeconds,
		Nodes:         make([]metering.CoverageNode, 0),
	}
	if subscriptionID == 0 {
		return summary, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	var nodeIDs []uint
	if err := db.Model(&model.ProtocolCredential{}).
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

	var rows []CoverageRecord
	if err := db.Where("node_id IN ?", nodeIDs).Find(&rows).Error; err != nil {
		return summary, err
	}
	byNode := make(map[uint]CoverageRecord, len(rows))
	for _, row := range rows {
		byNode[row.NodeID] = row
	}

	cutoff := now.Add(-time.Duration(windowSeconds) * time.Second)
	freshCutoff := now.Add(-(30 * time.Second))
	hasIncomplete := false
	hasUnknown := false
	for _, nodeID := range nodeIDs {
		row, ok := byNode[nodeID]
		state, reason := metering.ClassifyCoverage(metering.CoverageFact(row), ok, cutoff, freshCutoff)
		detail := metering.CoverageNode{
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
