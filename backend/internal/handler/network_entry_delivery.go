package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Build entries from explicit group grants, never by expanding the direct
// endpoint list. A landing credential alone is not a grant to any entry.
func (h *handlers) buildAuthorizedNetworkEntries(subscriptions []model.Subscription, filter subscriptionProjectionFilter, now time.Time) ([]subscriptionManifestNode, error) {
	result := []subscriptionManifestNode{}
	seen := map[uint]bool{}
	for _, sub := range subscriptions {
		var entries []model.NetworkEntry
		if err := h.db.Model(&model.NetworkEntry{}).Select("network_entries.*").
			Joins("JOIN node_group_network_entries membership ON membership.network_entry_id = network_entries.id").
			Where("membership.node_group_id = ? AND network_entries.enabled = ?", sub.NodeGroupID, true).
			Order("membership.sort_order, network_entries.id").Find(&entries).Error; err != nil {
			return nil, err
		}
		for _, entry := range entries {
			// Stale credentials and static protocol templates cannot bypass an
			// explicit landing grant, even before reconciliation runs.
			var grants int64
			if err := h.db.Model(&model.NodeGroupEndpoint{}).Where("node_group_id = ? AND protocol_endpoint_id = ?", sub.NodeGroupID, entry.EndpointID).Count(&grants).Error; err != nil {
				return nil, err
			}
			if grants == 0 {
				continue
			}
			if seen[entry.ID] {
				continue
			}
			var endpoint model.ProtocolEndpoint
			if err := h.db.Where("id = ? AND is_active = ?", entry.EndpointID, true).First(&endpoint).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return nil, err
			}
			var nodes []model.Node
			if err := h.db.Where("id IN ? AND is_enabled = ? AND last_seen_at >= ? AND lifecycle_status <> ?", []uint{entry.NodeID, endpoint.NodeID}, true, now.Add(-nodeOnlineWindow), resourceStatusDeleting).Find(&nodes).Error; err != nil {
				return nil, err
			}
			if len(nodes) != 2 {
				continue
			}
			var landing model.Node
			for _, node := range nodes {
				if node.ID == endpoint.NodeID {
					landing = node
				}
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
			var pending int64
			if err := h.db.Model(&model.NodeConfigPublish{}).Where("node_id IN ?", []uint{entry.NodeID, endpoint.NodeID}).Count(&pending).Error; err != nil {
				return nil, err
			}
			if pending > 0 {
				continue
			}
			base := subscriptionManifestNode{ID: endpoint.ID, NodeID: endpoint.NodeID, SubscriptionID: sub.ID,
				Name: endpoint.Name, Region: landing.Region, Address: endpoint.Address, Port: endpoint.Port, PublicPort: endpoint.PublicPort,
				Protocol: endpoint.Protocol, MultiplierMilli: endpoint.MultiplierMilli}
			if h.endpointDeliversSubscriptionCredential(endpoint) {
				var credential model.ProtocolCredential
				if err := h.db.Where("subscription_id = ? AND protocol_endpoint_id = ? AND status = ? AND revoked_at IS NULL AND expires_at > ?", sub.ID, endpoint.ID, protocolCredentialStatusActive, now).First(&credential).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						continue
					}
					return nil, err
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
