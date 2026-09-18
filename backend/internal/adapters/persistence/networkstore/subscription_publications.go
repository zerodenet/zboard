package networkstore

import (
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func EnqueueNodePublication(tx *gorm.DB, nodeID, endpointID, requestedBy uint) error {
	if nodeID == 0 {
		return nil
	}
	var entryNodes []uint
	if err := tx.Model(&model.NetworkEntry{}).Where("endpoint_id IN (?)", tx.Model(&model.ProtocolEndpoint{}).Select("id").Where("node_id = ?", nodeID)).Distinct().Pluck("node_id", &entryNodes).Error; err != nil {
		return err
	}
	for _, entryNode := range entryNodes {
		if entryNode != nodeID {
			if err := EnqueuePublication(tx, entryNode, 0, requestedBy); err != nil {
				return err
			}
		}
	}
	return EnqueuePublication(tx, nodeID, endpointID, requestedBy)
}

func EnqueueSubscriptionPublications(tx *gorm.DB, subscriptionID, requestedBy uint) error {
	var nodes []struct {
		NodeID     uint
		EndpointID uint
	}
	if err := tx.Table("protocol_endpoints").Select("protocol_endpoints.node_id, MIN(protocol_endpoints.id) AS endpoint_id").
		Joins("JOIN node_group_endpoints ON node_group_endpoints.protocol_endpoint_id = protocol_endpoints.id").
		Joins("JOIN subscriptions ON subscriptions.node_group_id = node_group_endpoints.node_group_id").
		Where("subscriptions.id = ? AND protocol_endpoints.is_active = ?", subscriptionID, true).
		Group("protocol_endpoints.node_id").Order("protocol_endpoints.node_id").Scan(&nodes).Error; err != nil {
		return err
	}
	for _, node := range nodes {
		if err := EnqueueNodePublication(tx, node.NodeID, node.EndpointID, requestedBy); err != nil {
			return err
		}
	}
	return nil
}
