package networkstore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type RuntimeConfiguration struct {
	DB *gorm.DB
}

func (s RuntimeConfiguration) LoadRuntimeConfiguration(ctx context.Context, nodeID uint, now time.Time, credentialProtocols []string) (network.RuntimeConfigurationSnapshot, error) {
	if s.DB == nil {
		return network.RuntimeConfigurationSnapshot{}, network.ErrRuntimeConfigurationUnavailable
	}
	var result network.RuntimeConfigurationSnapshot
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var installation model.Installation
		if err := tx.Select("site_url").First(&installation, 1).Error; err != nil {
			return err
		}
		result.SiteURL = installation.SiteURL

		var endpoints []model.ProtocolEndpoint
		if err := tx.Where("node_id = ? AND is_active = ?", nodeID, true).
			Order("id asc").Find(&endpoints).Error; err != nil {
			return err
		}
		result.Endpoints = make([]network.RuntimeConfigurationEndpoint, 0, len(endpoints))
		endpointIDs := make([]uint, 0, len(endpoints))
		for _, endpoint := range endpoints {
			endpointIDs = append(endpointIDs, endpoint.ID)
		}

		type certificateRow struct {
			ProtocolEndpointID uint
			Status             string
			CertPath           string
			KeyPath            string
			NotAfter           *time.Time
		}
		var certificates []certificateRow
		if err := tx.Table("certificate_protocol_endpoints").
			Select("certificate_protocol_endpoints.protocol_endpoint_id, managed_certificates.status, managed_certificates.cert_path, managed_certificates.key_path, managed_certificates.not_after").
			Joins("JOIN managed_certificates ON managed_certificates.id = certificate_protocol_endpoints.managed_certificate_id").
			Where("certificate_protocol_endpoints.protocol_endpoint_id IN ?", endpointIDs).
			Scan(&certificates).Error; err != nil {
			return err
		}
		certificateByEndpoint := make(map[uint]network.RuntimeConfigurationCertificate, len(certificates))
		for _, row := range certificates {
			certificateByEndpoint[row.ProtocolEndpointID] = network.RuntimeConfigurationCertificate{
				Status: row.Status, CertPath: row.CertPath, KeyPath: row.KeyPath, NotAfter: row.NotAfter,
			}
		}

		type subscriptionCountRow struct {
			ProtocolEndpointID uint
			Count              int64
		}
		var subscriptionCounts []subscriptionCountRow
		if err := tx.Table("node_group_endpoints").
			Select("node_group_endpoints.protocol_endpoint_id, COUNT(DISTINCT subscriptions.id) AS count").
			Joins("JOIN subscriptions ON subscriptions.node_group_id = node_group_endpoints.node_group_id").
			Where("node_group_endpoints.protocol_endpoint_id IN ?", endpointIDs).
			Where("subscriptions.status = ? AND subscriptions.end_at > ? AND subscriptions.flow_used < subscriptions.flow_total", "active", now).
			Group("node_group_endpoints.protocol_endpoint_id").Scan(&subscriptionCounts).Error; err != nil {
			return err
		}
		activeSubscriptionCount := make(map[uint]int64, len(subscriptionCounts))
		for _, row := range subscriptionCounts {
			activeSubscriptionCount[row.ProtocolEndpointID] = row.Count
		}

		type credentialRow struct {
			ID                    uint
			SubscriptionID        uint
			ProtocolEndpointID    uint
			PrincipalKey          string
			Secret                string
			ListenPort            int
			PublicPort            int
			SubscriptionUpdatedAt time.Time
			SpeedLimitMbps        int
			DeviceLimit           int
		}
		var credentials []credentialRow
		if err := tx.Table("protocol_credentials").
			Select("protocol_credentials.id, protocol_credentials.subscription_id, protocol_credentials.protocol_endpoint_id, protocol_credentials.principal_key, protocol_credentials.secret, protocol_credentials.listen_port, protocol_credentials.public_port, subscriptions.updated_at AS subscription_updated_at, subscriptions.speed_limit_mbps, subscriptions.device_limit").
			Joins("JOIN subscriptions ON subscriptions.id = protocol_credentials.subscription_id").
			Joins("JOIN node_group_endpoints ON node_group_endpoints.node_group_id = subscriptions.node_group_id AND node_group_endpoints.protocol_endpoint_id = protocol_credentials.protocol_endpoint_id").
			Where("protocol_credentials.protocol_endpoint_id IN ?", endpointIDs).
			Where("protocol_credentials.status = ? AND protocol_credentials.revoked_at IS NULL AND protocol_credentials.expires_at > ?", "active", now).
			Where("subscriptions.status = ? AND subscriptions.end_at > ? AND subscriptions.flow_used < subscriptions.flow_total", "active", now).
			Order("protocol_credentials.id asc").Scan(&credentials).Error; err != nil {
			return err
		}
		subscriptionIDs := make([]uint, 0, len(credentials))
		seenSubscriptions := make(map[uint]struct{}, len(credentials))
		for _, row := range credentials {
			if _, exists := seenSubscriptions[row.SubscriptionID]; exists {
				continue
			}
			seenSubscriptions[row.SubscriptionID] = struct{}{}
			subscriptionIDs = append(subscriptionIDs, row.SubscriptionID)
		}
		type credentialCountRow struct {
			SubscriptionID uint
			Count          int64
		}
		var credentialCounts []credentialCountRow
		if len(subscriptionIDs) > 0 {
			if err := tx.Table("protocol_credentials").
				Select("protocol_credentials.subscription_id, COUNT(*) AS count").
				Joins("JOIN protocol_endpoints ON protocol_endpoints.id = protocol_credentials.protocol_endpoint_id").
				Where("protocol_credentials.subscription_id IN ? AND protocol_credentials.status = ? AND protocol_credentials.revoked_at IS NULL AND protocol_credentials.expires_at > ?", subscriptionIDs, "active", now).
				Where("LOWER(protocol_endpoints.protocol) IN ?", credentialProtocols).
				Group("protocol_credentials.subscription_id").Scan(&credentialCounts).Error; err != nil {
				return err
			}
		}
		activeCredentialCount := make(map[uint]int64, len(credentialCounts))
		for _, row := range credentialCounts {
			activeCredentialCount[row.SubscriptionID] = row.Count
		}
		credentialsByEndpoint := make(map[uint][]network.RuntimeConfigurationCredential)
		for _, row := range credentials {
			credentialsByEndpoint[row.ProtocolEndpointID] = append(credentialsByEndpoint[row.ProtocolEndpointID], network.RuntimeConfigurationCredential{
				ID: row.ID, SubscriptionID: row.SubscriptionID, PrincipalKey: row.PrincipalKey, Secret: row.Secret,
				ListenPort: row.ListenPort, PublicPort: row.PublicPort, SubscriptionUpdatedAt: row.SubscriptionUpdatedAt,
				SpeedLimitMbps: row.SpeedLimitMbps, DeviceLimit: row.DeviceLimit,
				SoleActiveCredential: activeCredentialCount[row.SubscriptionID] == 1,
			})
		}

		for _, endpoint := range endpoints {
			item := network.RuntimeConfigurationEndpoint{
				ID: endpoint.ID, NodeID: endpoint.NodeID, Protocol: endpoint.Protocol, Address: endpoint.Address,
				Port: endpoint.Port, PublicPort: endpoint.PublicPort, MieruPrincipalReady: endpoint.MieruPrincipalReady,
				ServerConfig: endpoint.ServerConfig, ActiveSubscriptionCount: activeSubscriptionCount[endpoint.ID],
				Credentials: credentialsByEndpoint[endpoint.ID],
			}
			if certificate, exists := certificateByEndpoint[endpoint.ID]; exists {
				copy := certificate
				item.Certificate = &copy
			}
			result.Endpoints = append(result.Endpoints, item)
		}

		var entries []model.NetworkEntry
		if err := tx.Where("node_id = ? AND enabled = ?", nodeID, true).Order("id asc").Find(&entries).Error; err != nil {
			return err
		}
		landingIDs := make([]uint, 0, len(entries))
		poolIDs := make([]uint, 0, len(entries))
		for _, entry := range entries {
			landingIDs = append(landingIDs, entry.EndpointID)
			if entry.ProxyPoolID != nil {
				poolIDs = append(poolIDs, *entry.ProxyPoolID)
			}
		}
		var landings []model.ProtocolEndpoint
		if len(landingIDs) > 0 {
			if err := tx.Where("id IN ?", landingIDs).Find(&landings).Error; err != nil {
				return err
			}
		}
		landingByID := make(map[uint]model.ProtocolEndpoint, len(landings))
		landingNodeIDs := make([]uint, 0, len(landings))
		for _, landing := range landings {
			landingByID[landing.ID] = landing
			landingNodeIDs = append(landingNodeIDs, landing.NodeID)
		}
		var landingNodes []model.Node
		if len(landingNodeIDs) > 0 {
			if err := tx.Where("id IN ?", landingNodeIDs).Find(&landingNodes).Error; err != nil {
				return err
			}
		}
		landingNodeByID := make(map[uint]model.Node, len(landingNodes))
		for _, landingNode := range landingNodes {
			landingNodeByID[landingNode.ID] = landingNode
		}
		var pools []model.NodeProxyPool
		if len(poolIDs) > 0 {
			if err := tx.Where("id IN ?", poolIDs).Find(&pools).Error; err != nil {
				return err
			}
		}
		poolByID := make(map[uint]model.NodeProxyPool, len(pools))
		for _, pool := range pools {
			poolByID[pool.ID] = pool
		}
		result.NetworkEntries = make([]network.RuntimeConfigurationNetworkEntry, 0, len(entries))
		for _, entry := range entries {
			item := network.RuntimeConfigurationNetworkEntry{
				ID: entry.ID, NodeID: entry.NodeID, EndpointID: entry.EndpointID, Network: entry.Network,
				Port: entry.Port, ProxyPoolID: entry.ProxyPoolID, PathConfig: entry.PathConfig,
			}
			if landing, exists := landingByID[entry.EndpointID]; exists {
				item.Landing.EndpointExists = true
				item.Landing.Protocol = landing.Protocol
				item.Landing.Address = landing.Address
				item.Landing.Port = landing.Port
				item.Landing.PublicPort = landing.PublicPort
				item.Landing.Active = landing.IsActive
				if landingNode, exists := landingNodeByID[landing.NodeID]; exists {
					item.Landing.NodeExists = true
					item.Landing.NodeEnabled = landingNode.IsEnabled
					item.Landing.NodeLifecycleStatus = landingNode.LifecycleStatus
				}
			}
			if entry.ProxyPoolID != nil {
				if pool, exists := poolByID[*entry.ProxyPoolID]; exists {
					item.ProxyPool = &network.RuntimeConfigurationProxyPool{ID: pool.ID, NodeID: pool.NodeID, Config: pool.Config}
				}
			}
			result.NetworkEntries = append(result.NetworkEntries, item)
		}
		return nil
	})
	return result, err
}
