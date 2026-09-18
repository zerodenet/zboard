package meteringstore

import "time"

const (
	ScopeUser         = "user"
	ScopeSubscription = "subscription"
)

type PrincipalFlowObservation struct {
	ID                      uint64    `gorm:"column:id;primaryKey"`
	NodeID                  uint      `gorm:"column:node_id"`
	CoreInstanceID          string    `gorm:"column:core_instance_id"`
	SessionRegistryRevision uint64    `gorm:"column:session_registry_revision"`
	EventID                 string    `gorm:"column:event_id"`
	Sequence                uint64    `gorm:"column:sequence"`
	PrincipalKey            string    `gorm:"column:principal_key"`
	UserID                  uint      `gorm:"column:user_id"`
	SubscriptionID          uint      `gorm:"column:subscription_id"`
	ProtocolCredentialID    uint      `gorm:"column:protocol_credential_id"`
	ProtocolEndpointID      uint      `gorm:"column:protocol_endpoint_id"`
	ActiveFlows             uint64    `gorm:"column:active_flows"`
	ObservedAt              time.Time `gorm:"column:observed_at"`
	CreatedAt               time.Time `gorm:"column:created_at"`
}

func (PrincipalFlowObservation) TableName() string { return "principal_flow_observations" }

type PrincipalFlowCurrent struct {
	NodeID                  uint      `gorm:"column:node_id;primaryKey"`
	PrincipalKey            string    `gorm:"column:principal_key;primaryKey"`
	CoreInstanceID          string    `gorm:"column:core_instance_id"`
	SessionRegistryRevision uint64    `gorm:"column:session_registry_revision"`
	UserID                  uint      `gorm:"column:user_id"`
	SubscriptionID          uint      `gorm:"column:subscription_id"`
	ProtocolCredentialID    uint      `gorm:"column:protocol_credential_id"`
	ProtocolEndpointID      uint      `gorm:"column:protocol_endpoint_id"`
	ActiveFlows             uint64    `gorm:"column:active_flows"`
	ObservedAt              time.Time `gorm:"column:observed_at"`
	UpdatedAt               time.Time `gorm:"column:updated_at"`
}

func (PrincipalFlowCurrent) TableName() string { return "principal_flow_currents" }

type PrincipalFlowNodeGeneration struct {
	NodeID         uint       `gorm:"column:node_id;primaryKey"`
	CoreInstanceID string     `gorm:"column:core_instance_id"`
	StartedAt      time.Time  `gorm:"column:started_at"`
	ClosedAt       *time.Time `gorm:"column:closed_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
}

func (PrincipalFlowNodeGeneration) TableName() string { return "principal_flow_node_generations" }

type PrincipalFlowScopeCurrent struct {
	ScopeType   string    `gorm:"column:scope_type;primaryKey"`
	ScopeID     uint      `gorm:"column:scope_id;primaryKey"`
	ActiveFlows uint64    `gorm:"column:active_flows"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

func (PrincipalFlowScopeCurrent) TableName() string { return "principal_flow_scope_currents" }

type PrincipalFlowScopeObservation struct {
	ID                      uint64    `gorm:"column:id;primaryKey"`
	ScopeType               string    `gorm:"column:scope_type"`
	ScopeID                 uint      `gorm:"column:scope_id"`
	ActiveFlows             uint64    `gorm:"column:active_flows"`
	NodeID                  uint      `gorm:"column:node_id"`
	CoreInstanceID          string    `gorm:"column:core_instance_id"`
	SessionRegistryRevision uint64    `gorm:"column:session_registry_revision"`
	EventID                 string    `gorm:"column:event_id"`
	Source                  string    `gorm:"column:source"`
	ObservedAt              time.Time `gorm:"column:observed_at"`
	CreatedAt               time.Time `gorm:"column:created_at"`
}

func (PrincipalFlowScopeObservation) TableName() string {
	return "principal_flow_scope_observations"
}
