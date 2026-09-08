package model

import "time"

// NodeProxyPool belongs to one kernel. Its credentials never enter subscriptions.
type NodeProxyPool struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	NodeID    uint      `json:"node_id" gorm:"not null;uniqueIndex:ux_node_proxy_pool_name,priority:1"`
	Node      *Node     `json:"-" gorm:"foreignKey:NodeID;constraint:OnDelete:RESTRICT"`
	Name      string    `json:"name" gorm:"size:80;not null;uniqueIndex:ux_node_proxy_pool_name,priority:2"`
	Config    string    `json:"-" gorm:"type:text;not null"`
	Revision  uint64    `json:"revision" gorm:"not null;default:1"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
