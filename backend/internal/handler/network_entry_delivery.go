package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
)

// Build entries from explicit group grants, never by expanding the direct
// endpoint list. A landing credential alone is not a grant to any entry.
func (h *handlers) buildAuthorizedNetworkEntries(ctx context.Context, subscriptions []model.Subscription, filter subscriptionProjectionFilter, now time.Time) ([]subscriptionManifestNode, error) {
	targets := make([]entitlements.NetworkEntryProjectionSubscription, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		targets = append(targets, entitlements.NetworkEntryProjectionSubscription{ID: subscription.ID, NodeGroupID: subscription.NodeGroupID})
	}
	projection, err := h.services.NetworkEntryProjection.Load(ctx, targets, now)
	if err != nil {
		return nil, err
	}
	entriesByGroup := make(map[uint][]model.NetworkEntry)
	for _, row := range projection.Entries {
		entriesByGroup[row.NodeGroupID] = append(entriesByGroup[row.NodeGroupID], model.NetworkEntry{ID: row.ID, NodeID: row.NodeID, EndpointID: row.EndpointID, Name: row.Name, Address: row.Address, Network: row.Network, Port: row.Port, PublicPort: row.PublicPort, Enabled: true})
	}
	endpoints := make(map[uint]model.ProtocolEndpoint, len(projection.Endpoints))
	for _, row := range projection.Endpoints {
		endpoints[row.ID] = model.ProtocolEndpoint{ID: row.ID, NodeID: row.NodeID, Name: row.Name, Protocol: row.Protocol, Address: row.Address, ServerConfig: row.ServerConfig, ClientConfig: row.ClientConfig, Tags: row.Tags, Port: row.Port, PublicPort: row.PublicPort, MultiplierMilli: row.MultiplierMilli, ManagedPrincipalReady: row.ManagedPrincipalReady, MieruPrincipalReady: row.MieruPrincipalReady, IsActive: true}
	}
	nodes := make(map[uint]model.Node, len(projection.Nodes))
	for _, row := range projection.Nodes {
		nodes[row.ID] = model.Node{ID: row.ID, Region: row.Region, IsEnabled: row.IsEnabled, LifecycleStatus: row.LifecycleStatus, LastSeenAt: row.LastSeenAt}
	}
	pending := make(map[uint]struct{}, len(projection.PendingNodeIDs))
	for _, nodeID := range projection.PendingNodeIDs {
		pending[nodeID] = struct{}{}
	}
	type credentialKey struct{ subscriptionID, endpointID uint }
	credentials := make(map[credentialKey]model.ProtocolCredential, len(projection.Credentials))
	for _, row := range projection.Credentials {
		credentials[credentialKey{row.SubscriptionID, row.ProtocolEndpointID}] = model.ProtocolCredential{SubscriptionID: row.SubscriptionID, ProtocolEndpointID: row.ProtocolEndpointID, CredentialID: row.CredentialID, Secret: row.SecretCiphertext, ListenPort: row.ListenPort, PublicPort: row.PublicPort, Status: protocolCredentialStatusActive}
	}
	result := []subscriptionManifestNode{}
	seen := map[uint]bool{}
	for _, sub := range subscriptions {
		for _, entry := range entriesByGroup[sub.NodeGroupID] {
			if seen[entry.ID] {
				continue
			}
			endpoint, endpointExists := endpoints[entry.EndpointID]
			entryNode, entryNodeExists := nodes[entry.NodeID]
			landing, landingExists := nodes[endpoint.NodeID]
			if !endpointExists || !entryNodeExists || !landingExists || entryNode.ID == landing.ID {
				continue
			}
			if supported, _ := h.protocolKernelSupportForNode(endpoint.Protocol, landing); !supported {
				continue
			}
			visibleEndpoint := endpoint
			visibleEndpoint.Name = networkEntryDisplayName(entry.Name, endpoint.Name)
			visibleEndpoint.Address = entry.Address
			if !filter.matchesEndpoint(visibleEndpoint, landing) {
				continue
			}
			if _, exists := pending[entry.NodeID]; exists {
				continue
			}
			if _, exists := pending[endpoint.NodeID]; exists {
				continue
			}
			base := subscriptionManifestNode{ID: endpoint.ID, NodeID: endpoint.NodeID, SubscriptionID: sub.ID,
				Name: endpoint.Name, Region: landing.Region, Address: endpoint.Address, Port: endpoint.Port, PublicPort: endpoint.PublicPort,
				Protocol: endpoint.Protocol, MultiplierMilli: endpoint.MultiplierMilli}
			if h.endpointDeliversSubscriptionCredential(endpoint) {
				credential, exists := credentials[credentialKey{sub.ID, endpoint.ID}]
				if !exists {
					continue
				}
				config, err := h.credentialClientConfig(endpoint, credential)
				if err != nil {
					return nil, err
				}
				base.Config, base.CredentialID = config, credential.CredentialID
			} else {
				config, err := h.endpointSubscriptionClientConfig(endpoint)
				if err != nil {
					return nil, err
				}
				base.Config = config
			}
			front, err := projectNetworkEntry(base, entry)
			if err != nil {
				return nil, err
			}
			result = append(result, front)
			seen[entry.ID] = true
		}
	}
	return result, nil
}

func projectNetworkEntry(base subscriptionManifestNode, entry model.NetworkEntry) (subscriptionManifestNode, error) {
	var config map[string]interface{}
	if err := json.Unmarshal(base.Config, &config); err != nil {
		return base, err
	}
	if config == nil {
		return base, fmt.Errorf("落地协议 %d 缺少客户端配置", base.ID)
	}
	port := base.PublicPort
	if port <= 0 {
		port = base.Port
	}
	preserveNetworkEntryPeerIdentity(config, base.Address, base.Protocol, port)
	config["server"], config["port"] = entry.Address, entry.PublicPort
	raw, err := json.Marshal(config)
	if err != nil {
		return base, err
	}
	front := base
	front.NetworkEntryID, front.NetworkEntryNetwork = entry.ID, entry.Network
	front.Name = networkEntryDisplayName(entry.Name, base.Name)
	front.Address, front.Port, front.PublicPort, front.Config = entry.Address, entry.Port, entry.PublicPort, raw
	return front, nil
}

func networkEntryDisplayName(name, fallback string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return fallback
}
