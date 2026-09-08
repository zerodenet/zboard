package model

import "time"

// NodeGroupNetworkEntry grants delivery of one entry/landing combination.
// It does not grant delivery of the landing endpoint's direct address.
type NodeGroupNetworkEntry struct {
	Group          *NodeGroup    `json:"-" gorm:"foreignKey:NodeGroupID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
	Entry          *NetworkEntry `json:"-" gorm:"foreignKey:NetworkEntryID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
	ID             uint          `json:"id" gorm:"primaryKey"`
	NodeGroupID    uint          `json:"node_group_id" gorm:"uniqueIndex:ux_node_group_network_entry,priority:1;index;not null"`
	NetworkEntryID uint          `json:"network_entry_id" gorm:"uniqueIndex:ux_node_group_network_entry,priority:2;index;not null"`
	SortOrder      int           `json:"sort_order" gorm:"not null;default:0"`
	CreatedAt      time.Time     `json:"created_at"`
}
