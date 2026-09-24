package handler

import (
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type nodeServiceCount struct {
	NodeID uint
	Count  int64
}

// enabledNodeProtocolServiceCounts includes both protocol listeners and network
// fronting entries. A fronting entry is counted on the node that accepts the
// client connection, not on the landing endpoint's node.
func enabledNodeProtocolServiceCounts(db *gorm.DB, nodeIDs []uint) (map[uint]int64, error) {
	result := make(map[uint]int64, len(nodeIDs))
	if len(nodeIDs) == 0 {
		return result, nil
	}

	queries := []*gorm.DB{
		db.Model(&model.ProtocolEndpoint{}).Where("node_id IN ? AND is_active = ?", nodeIDs, true),
		db.Model(&model.NetworkEntry{}).Where("node_id IN ? AND enabled = ?", nodeIDs, true),
	}
	for _, query := range queries {
		counts := make([]nodeServiceCount, 0, len(nodeIDs))
		if err := query.Select("node_id, COUNT(*) AS count").Group("node_id").Scan(&counts).Error; err != nil {
			return nil, err
		}
		for _, count := range counts {
			result[count.NodeID] += count.Count
		}
	}
	return result, nil
}
