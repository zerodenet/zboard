package model

import "time"

// NetworkEntry forwards opaque protocol traffic. Authentication and charging remain at EndpointID.
type NetworkEntry struct {
	ProxyPoolID     *uint             `json:"proxy_pool_id" gorm:"index"`
	ProxyPool       *NodeProxyPool    `json:"-" gorm:"foreignKey:ProxyPoolID;constraint:OnDelete:RESTRICT"`
	EntryNode       *Node             `json:"-" gorm:"foreignKey:NodeID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
	LandingEndpoint *ProtocolEndpoint `json:"-" gorm:"foreignKey:EndpointID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
	Network         string            `json:"network" gorm:"size:16;not null;default:tcp_udp"`
	ID              uint              `json:"id" gorm:"primaryKey"`
	Name            string            `json:"name" gorm:"size:80;not null;uniqueIndex:ux_network_entry_name,priority:2"`
	NodeID          uint              `json:"node_id" gorm:"uniqueIndex:ux_network_entry_port,priority:1;not null"`
	EndpointID      uint              `json:"endpoint_id" gorm:"index;not null;uniqueIndex:ux_network_entry_name,priority:1"`
	Address         string            `json:"address" gorm:"size:255;not null"`
	Port            int               `json:"port" gorm:"uniqueIndex:ux_network_entry_port,priority:2;not null"`
	PublicPort      int               `json:"public_port" gorm:"not null"`
	Enabled         bool              `json:"enabled" gorm:"not null"`
	PathConfig      string            `json:"-" gorm:"type:text"`
	Revision        uint64            `json:"revision" gorm:"not null;default:1"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}
