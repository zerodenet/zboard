package credentialstore

import (
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type DeliveryProjection struct {
	ConfirmationEndpointSHA                                       string
	BindingNodeID                                                 uint
	BindingProtocol, BindingAddress, BindingListenAddress         string
	BindingPort, BindingPublicPort                                int
	BindingServerConfig, BindingClientConfig, BindingEgressConfig string
	model.ProtocolCredential                                      `gorm:"embedded"`
	ConfirmationNodeID                                            uint
	ConfirmationEndpointID                                        uint
	ConfirmationMaterial                                          string
	ConfirmationSHA                                               string
	ConfirmationAt                                                *time.Time
	TrackingNodeID                                                *uint
	TrackingSHA                                                   string
}

// ProjectedCredentials keeps the existing fixed query budget. Both auxiliary
// joins use primary keys and never read diagnostic or observation history.
func ProjectedCredentials(db *gorm.DB) *gorm.DB {
	return db.Model(&model.ProtocolCredential{}).Table("protocol_credentials").
		Joins("LEFT JOIN protocol_endpoints AS binding_endpoint ON binding_endpoint.id = protocol_credentials.protocol_endpoint_id").
		Joins("LEFT JOIN credential_delivery_confirmations AS delivery ON delivery.credential_id = protocol_credentials.id").
		Joins("LEFT JOIN node_credential_delivery_states AS delivery_state ON delivery_state.node_id = protocol_credentials.node_id").
		Select("protocol_credentials.*, delivery.node_id AS confirmation_node_id, delivery.protocol_endpoint_id AS confirmation_endpoint_id, binding_endpoint.node_id AS binding_node_id, binding_endpoint.protocol AS binding_protocol, binding_endpoint.address AS binding_address, binding_endpoint.listen_address AS binding_listen_address,binding_endpoint.port AS binding_port,binding_endpoint.public_port AS binding_public_port,binding_endpoint.server_config AS binding_server_config,binding_endpoint.client_config AS binding_client_config,binding_endpoint.egress_config AS binding_egress_config, delivery.endpoint_sha256 AS confirmation_endpoint_sha, delivery.material_sha256 AS confirmation_material, delivery.config_sha256 AS confirmation_sha, delivery.confirmed_at AS confirmation_at, delivery_state.node_id AS tracking_node_id, delivery_state.config_sha256 AS tracking_sha")
}
func (p DeliveryProjection) DeliveryStatus() string {
	status := "legacy_unknown"
	if p.DeliveryRequired || p.TrackingNodeID != nil {
		status = "pending"
	}
	material := network.CredentialMaterialFingerprint(p.NodeID, p.ProtocolEndpointID, p.ID, p.PrincipalKey, p.Secret, p.ListenPort, p.PublicPort)
	endpointSHA := network.CredentialEndpointFingerprint(network.RuntimeConfigurationEndpoint{ID: p.ProtocolEndpointID, NodeID: p.BindingNodeID, Protocol: p.BindingProtocol, Address: p.BindingAddress, ListenAddress: p.BindingListenAddress, Port: p.BindingPort, PublicPort: p.BindingPublicPort, ServerConfig: p.BindingServerConfig, ClientConfig: p.BindingClientConfig, EgressConfig: p.BindingEgressConfig})
	if p.TrackingNodeID != nil && material != "" && p.ConfirmationNodeID == p.NodeID && p.ConfirmationEndpointID == p.ProtocolEndpointID && p.ConfirmationMaterial == material && p.ConfirmationEndpointSHA == endpointSHA && len(p.TrackingSHA) == 64 && p.ConfirmationSHA == p.TrackingSHA && p.ConfirmationAt != nil && !p.ConfirmationAt.IsZero() {
		status = "confirmed"
	}
	return status
}

func DeliveryStatuses(db *gorm.DB, credentials []model.ProtocolCredential) (map[uint]string, error) {
	result := make(map[uint]string, len(credentials))
	for start := 0; start < len(credentials); start += 256 {
		end := start + 256
		if end > len(credentials) {
			end = len(credentials)
		}
		ids := make([]uint, 0, end-start)
		for _, row := range credentials[start:end] {
			ids = append(ids, row.ID)
			result[row.ID] = "pending"
		}
		var rows []DeliveryProjection
		if err := ProjectedCredentials(db).Where("protocol_credentials.id IN ?", ids).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			result[row.ID] = row.DeliveryStatus()
		}
	}
	return result, nil
}
