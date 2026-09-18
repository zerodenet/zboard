package networkstore

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"sort"
)

// Exactly one selector is used by each owning resource transaction.
type TopologySelection struct{ NodeID, EndpointID, EntryID uint }

func RemoveTopologyEntries(tx *gorm.DB, actor uint, selection TopologySelection) (int64, error) {
	selected := 0
	for _, id := range []uint{selection.NodeID, selection.EndpointID, selection.EntryID} {
		if id != 0 {
			selected++
		}
	}
	if selected != 1 {
		return 0, errors.New("exactly one topology removal selector is required")
	}
	query := tx.Model(&model.NetworkEntry{})
	switch {
	case selection.NodeID != 0:
		query = query.Where("node_id = ? OR endpoint_id IN (?)", selection.NodeID, tx.Model(&model.ProtocolEndpoint{}).Select("id").Where("node_id = ?", selection.NodeID))
	case selection.EndpointID != 0:
		query = query.Where("endpoint_id = ?", selection.EndpointID)
	default:
		query = query.Where("id = ?", selection.EntryID)
	}
	var entries []model.NetworkEntry
	if err := query.Find(&entries).Error; err != nil {
		return 0, err
	}
	if len(entries) == 0 {
		return 0, nil
	}
	ids := make([]uint, 0, len(entries))
	nodes := map[uint]bool{}
	for _, entry := range entries {
		ids = append(ids, entry.ID)
		if entry.NodeID != selection.NodeID {
			nodes[entry.NodeID] = true
		}
	}
	groups := tx.Model(&model.NodeGroupNetworkEntry{}).Select("node_group_id").Where("network_entry_id IN ?", ids)
	if err := tx.Model(&model.NodeGroup{}).Where("id IN (?)", groups).Update("revision", gorm.Expr("revision + 1")).Error; err != nil {
		return 0, err
	}
	if err := tx.Where("network_entry_id IN ?", ids).Delete(&model.NodeGroupNetworkEntry{}).Error; err != nil {
		return 0, err
	}
	if err := tx.Where("id IN ?", ids).Delete(&model.NetworkEntry{}).Error; err != nil {
		return 0, err
	}
	surviving := make([]uint, 0, len(nodes))
	for node := range nodes {
		surviving = append(surviving, node)
	}
	sort.Slice(surviving, func(i, j int) bool { return surviving[i] < surviving[j] })
	for _, node := range surviving {
		if err := EnqueuePublication(tx, node, 0, actor); err != nil {
			return 0, err
		}
	}
	return int64(len(entries)), nil
}
func TouchEndpointGroups(tx *gorm.DB, endpointIDs []uint) error {
	if len(endpointIDs) == 0 {
		return nil
	}
	groups := tx.Model(&model.NodeGroupEndpoint{}).Select("node_group_id").Where("protocol_endpoint_id IN ?", endpointIDs)
	return tx.Model(&model.NodeGroup{}).Where("id IN (?)", groups).Update("revision", gorm.Expr("revision + 1")).Error
}
