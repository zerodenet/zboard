package model

import "time"

// NodeProxyPool is reusable infrastructure configuration. NodeID is a nullable
// legacy origin only; node_proxy_pool_bindings owns server references. Pool and
// source credentials never enter client-facing subscription output.
type NodeProxyPool struct {
	ID                    uint       `json:"id" gorm:"primaryKey"`
	NodeID                uint       `json:"node_id" gorm:"uniqueIndex:ux_node_proxy_pool_name,priority:1"`
	Node                  *Node      `json:"-" gorm:"foreignKey:NodeID;constraint:OnDelete:RESTRICT"`
	Name                  string     `json:"name" gorm:"size:80;not null;uniqueIndex:ux_node_proxy_pool_name,priority:2"`
	Config                string     `json:"-" gorm:"type:text;not null"`
	SubscriptionURL       string     `json:"-" gorm:"type:text;not null"`
	SubscriptionFormat    string     `json:"subscription_format,omitempty" gorm:"size:32;not null"`
	SubscriptionUserAgent string     `json:"-" gorm:"size:255;not null"`
	AutoSync              bool       `json:"auto_sync" gorm:"not null;default:false;index:idx_node_proxy_pool_sync_due,priority:1"`
	SyncIntervalSeconds   int        `json:"sync_interval_seconds" gorm:"not null;default:86400"`
	SubscriptionNodeCount int        `json:"subscription_node_count" gorm:"not null;default:0"`
	LastSyncAt            *time.Time `json:"last_sync_at,omitempty"`
	NextSyncAt            *time.Time `json:"next_sync_at,omitempty" gorm:"index:idx_node_proxy_pool_sync_due,priority:2"`
	LastSyncError         string     `json:"last_sync_error,omitempty" gorm:"size:1000;not null"`
	Revision              uint64     `json:"revision" gorm:"not null;default:1"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

// A binding makes a pool available on a server without changing any route.
// NetworkEntry.ProxyPoolID remains the explicit authority for a forwarding path.
type NodeProxyPoolBinding struct {
	NodeID      uint           `json:"node_id" gorm:"primaryKey;autoIncrement:false"`
	ProxyPoolID uint           `json:"proxy_pool_id" gorm:"primaryKey;autoIncrement:false;index"`
	Node        *Node          `json:"-" gorm:"foreignKey:NodeID;constraint:OnDelete:CASCADE"`
	ProxyPool   *NodeProxyPool `json:"-" gorm:"foreignKey:ProxyPoolID;constraint:OnDelete:CASCADE"`
	CreatedAt   time.Time      `json:"created_at"`
}
