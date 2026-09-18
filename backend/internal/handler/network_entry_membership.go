package handler

import (
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Only explicit protocol membership grants credentials. A forward target is
// topology, never an implicit authorization edge.
const credentialMembershipSQL = `(SELECT node_group_id, protocol_endpoint_id FROM node_group_endpoints)`

func credentialMemberships(db *gorm.DB) *gorm.DB {
	return db.Table(credentialMembershipSQL + " AS node_group_endpoints")
}

func credentialMembershipJoin(condition string) string {
	return "JOIN " + credentialMembershipSQL + " AS node_group_endpoints ON " + condition
}

func loadNodeGroupNetworkEntryIDs(db *gorm.DB, group *model.NodeGroup) error {
	group.NetworkEntryIDs = []uint{}
	return db.Model(&model.NodeGroupNetworkEntry{}).Where("node_group_id = ?", group.ID).
		Order("sort_order, id").Pluck("network_entry_id", &group.NetworkEntryIDs).Error
}

func loadNetworkEntryMemberships(db *gorm.DB, entryID uint) ([]protocolEndpointNodeGroupMembership, error) {
	rows := []protocolEndpointNodeGroupMembership{}
	err := db.Table("node_group_network_entries membership").Select("membership.node_group_id, node_groups.name, node_groups.code, node_groups.description, node_groups.is_enabled, node_groups.revision, membership.sort_order").Joins("JOIN node_groups ON node_groups.id = membership.node_group_id").Where("membership.network_entry_id = ?", entryID).Order("node_groups.id").Scan(&rows).Error
	return rows, err
}
