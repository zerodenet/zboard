package entitlementstore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type SubscriptionProjection struct{ DB *gorm.DB }

func (s SubscriptionProjection) ProjectionSources(ctx context.Context, ids []uint) ([]entitlements.SubscriptionProjectionSource, error) {
	var rows []entitlements.SubscriptionProjectionSource
	err := s.DB.WithContext(ctx).Table("subscriptions").
		Select("subscriptions.id AS subscription_id, plans.slug AS plan_slug, plan_skus.code AS sku_code, node_groups.code AS node_group_code").
		Joins("JOIN plans ON plans.id = subscriptions.plan_id").
		Joins("JOIN plan_skus ON plan_skus.id = subscriptions.plan_sku_id").
		Joins("JOIN node_groups ON node_groups.id = subscriptions.node_group_id").
		Where("subscriptions.id IN ?", ids).Scan(&rows).Error
	return rows, err
}

func (s SubscriptionProjection) ProjectionData(ctx context.Context, subscriptionIDs, nodeGroupIDs []uint, now time.Time) (entitlements.SubscriptionProjectionData, error) {
	result := entitlements.SubscriptionProjectionData{Memberships: map[uint][]uint{}, Nodes: map[uint]entitlements.SubscriptionProjectionNode{}}
	type membershipRow struct{ NodeGroupID, ProtocolEndpointID uint }
	var memberships []membershipRow
	if err := s.DB.WithContext(ctx).Table("node_group_endpoints").Select("node_group_id, protocol_endpoint_id").Where("node_group_id IN ?", nodeGroupIDs).Scan(&memberships).Error; err != nil {
		return result, err
	}
	endpointIDs := make([]uint, 0, len(memberships))
	seenEndpoint := map[uint]struct{}{}
	for _, row := range memberships {
		result.Memberships[row.NodeGroupID] = append(result.Memberships[row.NodeGroupID], row.ProtocolEndpointID)
		if _, ok := seenEndpoint[row.ProtocolEndpointID]; !ok {
			seenEndpoint[row.ProtocolEndpointID] = struct{}{}
			endpointIDs = append(endpointIDs, row.ProtocolEndpointID)
		}
	}
	var credentials []model.ProtocolCredential
	if err := s.DB.WithContext(ctx).Where("subscription_id IN ? AND status = ? AND revoked_at IS NULL AND expires_at > ?", subscriptionIDs, "active", now).Find(&credentials).Error; err != nil {
		return result, err
	}
	result.Credentials = make([]entitlements.SubscriptionProjectionCredential, 0, len(credentials))
	for _, row := range credentials {
		result.Credentials = append(result.Credentials, entitlements.SubscriptionProjectionCredential{
			ID: row.ID, SubscriptionID: row.SubscriptionID, UserID: row.UserID, ProtocolEndpointID: row.ProtocolEndpointID, NodeID: row.NodeID,
			CredentialID: row.CredentialID, PrincipalKey: row.PrincipalKey, SecretCiphertext: row.Secret, Status: row.Status,
			ListenPort: row.ListenPort, PublicPort: row.PublicPort, ExpiresAt: row.ExpiresAt, LastUsedAt: row.LastUsedAt, RevokedAt: row.RevokedAt,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}
	if len(endpointIDs) == 0 {
		return result, nil
	}
	var endpoints []model.ProtocolEndpoint
	if err := s.DB.WithContext(ctx).Where("id IN ? AND is_active = ?", endpointIDs, true).Order("sort_order asc, id asc").Find(&endpoints).Error; err != nil {
		return result, err
	}
	nodeIDs := make([]uint, 0, len(endpoints))
	seenNode := map[uint]struct{}{}
	for _, row := range endpoints {
		result.Endpoints = append(result.Endpoints, entitlements.SubscriptionProjectionEndpoint{
			ID: row.ID, NodeID: row.NodeID, Name: row.Name, Protocol: row.Protocol, Address: row.Address,
			ServerConfig: row.ServerConfig, ClientConfig: row.ClientConfig, Tags: row.Tags, Port: row.Port, PublicPort: row.PublicPort,
			SortOrder: row.SortOrder, MultiplierMilli: row.MultiplierMilli, ManagedPrincipalReady: row.ManagedPrincipalReady, MieruPrincipalReady: row.MieruPrincipalReady,
		})
		if _, ok := seenNode[row.NodeID]; !ok {
			seenNode[row.NodeID] = struct{}{}
			nodeIDs = append(nodeIDs, row.NodeID)
		}
	}
	var nodes []model.Node
	if err := s.DB.WithContext(ctx).Select("id", "region", "is_enabled", "last_seen_at").Where("id IN ?", nodeIDs).Find(&nodes).Error; err != nil {
		return result, err
	}
	for _, row := range nodes {
		result.Nodes[row.ID] = entitlements.SubscriptionProjectionNode{ID: row.ID, Region: row.Region, IsEnabled: row.IsEnabled, LastSeenAt: row.LastSeenAt}
	}
	return result, nil
}
