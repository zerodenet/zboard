package entitlementstore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
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
		ID, NodeGroupID, NodeID, EndpointID uint
		Name, Address, Network              string
		Port, PublicPort                    int
	}
	var entries []entryRow
	if err := s.DB.WithContext(ctx).Table("node_group_network_entries AS membership").
		Select("network_entries.id, membership.node_group_id, network_entries.node_id, network_entries.endpoint_id, network_entries.name, network_entries.address, network_entries.network, network_entries.port, network_entries.public_port").
		Joins("JOIN network_entries ON network_entries.id = membership.network_entry_id").
		Joins("JOIN node_group_endpoints AS landing_grant ON landing_grant.node_group_id = membership.node_group_id AND landing_grant.protocol_endpoint_id = network_entries.endpoint_id").
		Where("membership.node_group_id IN ? AND network_entries.enabled = ?", groupIDs, true).
		Order("membership.node_group_id asc, membership.sort_order asc, network_entries.id asc").Scan(&entries).Error; err != nil {
		return result, err
	}
	endpointIDs := make([]uint, 0, len(entries))
	nodeIDs := make([]uint, 0, len(entries)*2)
	seenEndpoint, seenNode := map[uint]struct{}{}, map[uint]struct{}{}
	for _, row := range entries {
		result.Entries = append(result.Entries, entitlements.NetworkEntryProjectionEntry{ID: row.ID, NodeGroupID: row.NodeGroupID, NodeID: row.NodeID, EndpointID: row.EndpointID, Name: row.Name, Address: row.Address, Network: row.Network, Port: row.Port, PublicPort: row.PublicPort})
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
		Where("id IN ? AND is_enabled = ? AND last_seen_at >= ? AND lifecycle_status <> ?", nodeIDs, true, now.Add(-2*time.Minute), "deleting").Find(&nodes).Error; err != nil {
		return result, err
	}
	for _, row := range nodes {
		result.Nodes = append(result.Nodes, entitlements.NetworkEntryProjectionNode{ID: row.ID, Region: row.Region, IsEnabled: row.IsEnabled, LifecycleStatus: row.LifecycleStatus, LastSeenAt: row.LastSeenAt})
	}
	if err := s.DB.WithContext(ctx).Model(&model.NodeConfigPublish{}).Where("node_id IN ?", nodeIDs).Distinct().Pluck("node_id", &result.PendingNodeIDs).Error; err != nil {
		return result, err
	}
	var credentials []model.ProtocolCredential
	if err := s.DB.WithContext(ctx).Where("subscription_id IN ? AND protocol_endpoint_id IN ? AND status = ? AND revoked_at IS NULL AND expires_at > ?", subscriptionIDs, endpointIDs, "active", now).Find(&credentials).Error; err != nil {
		return result, err
	}
	for _, row := range credentials {
		result.Credentials = append(result.Credentials, entitlements.NetworkEntryProjectionCredential{SubscriptionID: row.SubscriptionID, ProtocolEndpointID: row.ProtocolEndpointID, CredentialID: row.CredentialID, SecretCiphertext: row.Secret, ListenPort: row.ListenPort, PublicPort: row.PublicPort})
	}
	return result, nil
}

var _ entitlements.NetworkEntryProjectionRepository = NetworkEntryProjection{}
