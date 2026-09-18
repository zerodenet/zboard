package meteringstore

import "time"

type FlowStartRecord struct {
	ID                   uint64    `gorm:"column:id;primaryKey"`
	NodeID               uint      `gorm:"column:node_id"`
	CoreInstanceID       string    `gorm:"column:core_instance_id"`
	EventID              string    `gorm:"column:event_id"`
	Sequence             uint64    `gorm:"column:sequence"`
	PrincipalKey         string    `gorm:"column:principal_key"`
	UserID               uint      `gorm:"column:user_id"`
	SubscriptionID       uint      `gorm:"column:subscription_id"`
	ProtocolCredentialID uint      `gorm:"column:protocol_credential_id"`
	ProtocolEndpointID   uint      `gorm:"column:protocol_endpoint_id"`
	MappingState         string    `gorm:"column:mapping_state"`
	OccurredAt           time.Time `gorm:"column:occurred_at"`
	ReceivedAt           time.Time `gorm:"column:received_at"`
	CreatedAt            time.Time `gorm:"column:created_at"`
}

func (FlowStartRecord) TableName() string { return "subscription_flow_start_events" }
