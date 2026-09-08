package handler

import (
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Local deletion removes topology and memberships atomically. Remote config
// withdrawal for surviving nodes is queued, never a prerequisite for deletion.
func deleteNetworkEntryRecords(tx *gorm.DB, deletingNodeID, requestedBy uint, query string, args ...interface{}) (int64, error) {
	var entries []model.NetworkEntry
	if err := tx.Where(query, args...).Find(&entries).Error; err != nil {
		return 0, err
	}
	for _, entry := range entries {
		groupIDs := tx.Model(&model.NodeGroupNetworkEntry{}).Select("node_group_id").Where("network_entry_id = ?", entry.ID)
		if err := tx.Model(&model.NodeGroup{}).Where("id IN (?)", groupIDs).Update("revision", gorm.Expr("revision + 1")).Error; err != nil {
			return 0, err
		}
		if err := tx.Where("network_entry_id = ?", entry.ID).Delete(&model.NodeGroupNetworkEntry{}).Error; err != nil {
			return 0, err
		}
		if err := tx.Delete(&entry).Error; err != nil {
			return 0, err
		}
		if entry.NodeID != deletingNodeID {
			if err := enqueueNodeConfigPublishOnly(tx, entry.NodeID, 0, requestedBy); err != nil {
				return 0, err
			}
		}
	}
	return int64(len(entries)), nil
}

func touchEndpointGroups(tx *gorm.DB, endpointIDs []uint) error {
	if len(endpointIDs) == 0 {
		return nil
	}
	groups := tx.Model(&model.NodeGroupEndpoint{}).Select("node_group_id").Where("protocol_endpoint_id IN ?", endpointIDs)
	return tx.Model(&model.NodeGroup{}).Where("id IN (?)", groups).Update("revision", gorm.Expr("revision + 1")).Error
}
