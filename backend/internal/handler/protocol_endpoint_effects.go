package handler

import networkcap "github.com/zerodenet/zboard/backend/internal/capabilities/network"

type protocolEndpointEffect = networkcap.ProtocolEndpointEffect

const (
	protocolEndpointEffectNone                = networkcap.ProtocolEndpointEffectNone
	protocolEndpointEffectManagement          = networkcap.ProtocolEndpointEffectManagement
	protocolEndpointEffectBilling             = networkcap.ProtocolEndpointEffectBilling
	protocolEndpointEffectDelivery            = networkcap.ProtocolEndpointEffectDelivery
	protocolEndpointEffectRuntime             = networkcap.ProtocolEndpointEffectRuntime
	protocolEndpointEffectCredentialPlacement = networkcap.ProtocolEndpointEffectCredentialPlacement

	protocolEndpointPublishNotRequired = networkcap.ProtocolEndpointPublishNotRequired
	protocolEndpointPublishQueued      = networkcap.ProtocolEndpointPublishQueued

	// Runtime compilation is ordered by endpoint identity. SortOrder belongs to subscription delivery.
	protocolEndpointRuntimeOrder = "id asc"
)

type protocolEndpointEffectSnapshot struct {
	NodeID               uint
	Name                 string
	Protocol             string
	Address              string
	Port                 int
	PublicPort           int
	Cipher               int16
	ParentProtocolID     *uint
	MultiplierMilli      int64
	ServerConfig         string
	ClientConfig         string
	OptionalConfig       string
	Tags                 string
	IsActive             bool
	SortOrder            int
	ManagedCertificateID uint
}

type protocolEndpointChangeEffects = networkcap.ProtocolEndpointChangeEffects

type protocolEndpointMutationResponse struct {
	ProtocolEndpoint     networkcap.ProtocolEndpointRecord              `json:"protocol_endpoint"`
	NodeGroupMemberships []networkcap.ProtocolEndpointMembership        `json:"node_group_memberships"`
	NodeGroupMembership  *networkcap.ProtocolEndpointMembershipMutation `json:"node_group_membership,omitempty"`
	Timing               protocolEndpointMutationTiming                 `json:"timing"`
	protocolEndpointChangeEffects
}

func classifyProtocolEndpointChange(before *protocolEndpointEffectSnapshot, after protocolEndpointEffectSnapshot) protocolEndpointChangeEffects {
	var beforeRecord *networkcap.ProtocolEndpointRecord
	beforeCertificateID := uint(0)
	if before != nil {
		record := protocolEndpointEffectRecord(*before)
		beforeRecord = &record
		beforeCertificateID = before.ManagedCertificateID
	}
	return networkcap.ClassifyProtocolEndpointChange(beforeRecord, protocolEndpointEffectRecord(after), beforeCertificateID, after.ManagedCertificateID)
}

func protocolEndpointEffectRecord(snapshot protocolEndpointEffectSnapshot) networkcap.ProtocolEndpointRecord {
	return networkcap.ProtocolEndpointRecord{
		NodeID: snapshot.NodeID, Name: snapshot.Name, Protocol: snapshot.Protocol, Address: snapshot.Address,
		Port: snapshot.Port, PublicPort: snapshot.PublicPort, Cipher: snapshot.Cipher,
		ParentProtocolID: snapshot.ParentProtocolID, MultiplierMilli: snapshot.MultiplierMilli,
		ServerConfig: snapshot.ServerConfig, ClientConfig: snapshot.ClientConfig,
		OptionalConfig: snapshot.OptionalConfig, Tags: snapshot.Tags, IsActive: snapshot.IsActive, SortOrder: snapshot.SortOrder,
	}
}
