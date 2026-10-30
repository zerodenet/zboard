package entitlementstore

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type NetworkEntryProjection struct{ DB *gorm.DB }

func (s NetworkEntryProjection) LoadNetworkEntryProjection(ctx context.Context, subscriptions []entitlements.NetworkEntryProjectionSubscription, now time.Time) (entitlements.NetworkEntryProjectionData, error) {
	result := entitlements.NetworkEntryProjectionData{}
	groupIDs := make([]uint, 0, len(subscriptions))
	subscriptionIDs := make([]uint, 0, len(subscriptions))
	seenGroup := make(map[uint]struct{}, len(subscriptions))
	for _, subscription := range subscriptions {
		subscriptionIDs = append(subscriptionIDs, subscription.ID)
		if _, exists := seenGroup[subscription.NodeGroupID]; !exists {
			seenGroup[subscription.NodeGroupID] = struct{}{}
			groupIDs = append(groupIDs, subscription.NodeGroupID)
		}
	}
	type entryRow struct {
		DeliveryRequired                      bool
		ListenAddress, PathConfig             string
		ProxyPoolID                           *uint
		PoolConfig                            string
		LandingProtocol, LandingAddress       string
		LandingPort, LandingPublicPort        int
		ConfirmationNodeID                    uint
		ConfirmationMaterial, ConfirmationSHA string
		TrackingNodeID                        *uint
		TrackingSHA                           string

		DeploymentMode                      string
		ID, NodeGroupID, NodeID, EndpointID uint
		Name, Address, Network              string
		Port, PublicPort                    int
	}
	var entries []entryRow
	if err := s.DB.WithContext(ctx).Table("node_group_network_entries AS membership").
		Select("network_entries.delivery_required, network_entries.listen_address,network_entries.path_config,network_entries.proxy_pool_id,delivery_pool.config AS pool_config,delivery_landing.protocol AS landing_protocol,delivery_landing.address AS landing_address,delivery_landing.port AS landing_port,delivery_landing.public_port AS landing_public_port,entry_confirmation.node_id AS confirmation_node_id,entry_confirmation.material_sha256 AS confirmation_material,entry_confirmation.config_sha256 AS confirmation_sha,entry_state.node_id AS tracking_node_id,entry_state.config_sha256 AS tracking_sha,network_entries.deployment_mode, network_entries.id, membership.node_group_id, network_entries.node_id, network_entries.endpoint_id, network_entries.name, network_entries.address, network_entries.network, network_entries.port, network_entries.public_port").
		Joins("JOIN network_entries ON network_entries.id = membership.network_entry_id").
		Joins("LEFT JOIN protocol_endpoints AS delivery_landing ON delivery_landing.id = network_entries.endpoint_id").
		Joins("LEFT JOIN node_proxy_pools AS delivery_pool ON delivery_pool.id = network_entries.proxy_pool_id").
		Joins("LEFT JOIN network_entry_delivery_confirmations AS entry_confirmation ON entry_confirmation.entry_id = network_entries.id").
		Joins("LEFT JOIN node_credential_delivery_states AS entry_state ON entry_state.node_id = network_entries.node_id").
		Where("membership.node_group_id IN ? AND network_entries.enabled = ?", groupIDs, true).
		Order("membership.node_group_id asc, membership.sort_order asc, network_entries.id asc").Scan(&entries).Error; err != nil {
		return result, err
	}
	endpointIDs := make([]uint, 0, len(entries))
	nodeIDs := make([]uint, 0, len(entries)*2)
	seenEndpoint, seenNode := map[uint]struct{}{}, map[uint]struct{}{}
	for _, row := range entries {
		status := "legacy_unknown"
		if row.DeploymentMode == "external" {
			status = "external_unverified"
		} else {
			if row.DeliveryRequired || row.TrackingNodeID != nil {
				status = "pending"
			}
			target := network.RuntimeConfigurationNetworkEntry{ID: row.ID, NodeID: row.NodeID, EndpointID: row.EndpointID, Network: row.Network, Address: row.Address, ListenAddress: row.ListenAddress, Port: row.Port, PublicPort: row.PublicPort, PathConfig: row.PathConfig, ProxyPoolID: row.ProxyPoolID, Landing: network.RuntimeConfigurationLanding{Protocol: row.LandingProtocol, Address: row.LandingAddress, Port: row.LandingPort, PublicPort: row.LandingPublicPort}}
			if row.ProxyPoolID != nil {
				target.ProxyPool = &network.RuntimeConfigurationProxyPool{ID: *row.ProxyPoolID, Config: row.PoolConfig}
			}
			if row.TrackingNodeID != nil && row.ConfirmationNodeID == row.NodeID && len(row.TrackingSHA) == 64 && row.ConfirmationSHA == row.TrackingSHA && row.ConfirmationMaterial == network.NetworkEntryDeliveryFingerprint(target) {
				status = "confirmed"
			}
		}
		result.Entries = append(result.Entries, entitlements.NetworkEntryProjectionEntry{DeliveryStatus: status, DeploymentMode: row.DeploymentMode, ID: row.ID, NodeGroupID: row.NodeGroupID, NodeID: row.NodeID, EndpointID: row.EndpointID, Name: row.Name, Address: row.Address, Network: row.Network, Port: row.Port, PublicPort: row.PublicPort})
		if _, exists := seenEndpoint[row.EndpointID]; !exists {
			seenEndpoint[row.EndpointID] = struct{}{}
			endpointIDs = append(endpointIDs, row.EndpointID)
		}
		if _, exists := seenNode[row.NodeID]; !exists {
			seenNode[row.NodeID] = struct{}{}
			nodeIDs = append(nodeIDs, row.NodeID)
		}
	}
	if len(endpointIDs) == 0 {
		return result, nil
	}
	var endpoints []model.ProtocolEndpoint
	if err := s.DB.WithContext(ctx).Where("id IN ? AND is_active = ?", endpointIDs, true).Find(&endpoints).Error; err != nil {
		return result, err
	}
	for _, row := range endpoints {
		result.Endpoints = append(result.Endpoints, entitlements.NetworkEntryProjectionEndpoint{ID: row.ID, NodeID: row.NodeID, Name: row.Name, Protocol: row.Protocol, Address: row.Address, ServerConfig: row.ServerConfig, ClientConfig: row.ClientConfig, Tags: row.Tags, Port: row.Port, PublicPort: row.PublicPort, MultiplierMilli: row.MultiplierMilli, ManagedPrincipalReady: row.ManagedPrincipalReady, MieruPrincipalReady: row.MieruPrincipalReady})
		if _, exists := seenNode[row.NodeID]; !exists {
			seenNode[row.NodeID] = struct{}{}
			nodeIDs = append(nodeIDs, row.NodeID)
		}
	}
	var nodes []model.Node
	if err := s.DB.WithContext(ctx).Select("id", "region", "is_enabled", "lifecycle_status", "last_seen_at").
		Where("id IN ? AND is_enabled = ? AND lifecycle_status NOT IN ?", nodeIDs, true, []string{"deleting", "archived"}).Find(&nodes).Error; err != nil {
		return result, err
	}
	for _, row := range nodes {
		result.Nodes = append(result.Nodes, entitlements.NetworkEntryProjectionNode{ID: row.ID, Region: row.Region, IsEnabled: row.IsEnabled, LifecycleStatus: row.LifecycleStatus, LastSeenAt: row.LastSeenAt})
	}
	var credentials []credentialstore.DeliveryProjection
	if err := credentialstore.ProjectedCredentials(s.DB.WithContext(ctx)).Where("protocol_credentials.subscription_id IN ? AND protocol_credentials.protocol_endpoint_id IN ? AND protocol_credentials.status = ? AND protocol_credentials.revoked_at IS NULL AND protocol_credentials.expires_at > ?", subscriptionIDs, endpointIDs, "active", now).Find(&credentials).Error; err != nil {
		return result, err
	}
	for _, row := range credentials {
		result.Credentials = append(result.Credentials, entitlements.NetworkEntryProjectionCredential{DeliveryStatus: row.DeliveryStatus(), SubscriptionID: row.SubscriptionID, ProtocolEndpointID: row.ProtocolEndpointID, CredentialID: row.CredentialID, SecretCiphertext: row.Secret, ListenPort: row.ListenPort, PublicPort: row.PublicPort})
	}
	return result, nil
}

var _ entitlements.NetworkEntryProjectionRepository = NetworkEntryProjection{}
