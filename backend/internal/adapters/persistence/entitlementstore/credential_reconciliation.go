package entitlementstore

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type GroupCredentialReconciliation struct {
	DB     *gorm.DB
	Issuer entitlements.CredentialIssuer
}

type NodeCredentialReconciliation struct {
	DB     *gorm.DB
	Issuer entitlements.CredentialIssuer
}

func (s NodeCredentialReconciliation) ReconcileNode(ctx context.Context, nodeID uint, now time.Time) error {
	if s.DB == nil || s.Issuer == nil {
		return entitlements.ErrCredentialReconciliationUnavailable
	}
	var groupIDs []uint
	if err := s.DB.WithContext(ctx).Model(&model.NodeGroupEndpoint{}).
		Distinct("node_group_endpoints.node_group_id").
		Joins("JOIN protocol_endpoints ON protocol_endpoints.id = node_group_endpoints.protocol_endpoint_id").
		Where("protocol_endpoints.node_id = ? AND protocol_endpoints.is_active = ?", nodeID, true).
		Order("node_group_endpoints.node_group_id asc").
		Pluck("node_group_endpoints.node_group_id", &groupIDs).Error; err != nil {
		return err
	}
	groups := GroupCredentialReconciliation{DB: s.DB, Issuer: s.Issuer}
	for _, groupID := range groupIDs {
		if err := groups.ReconcileGroup(ctx, groupID, now); err != nil {
			return err
		}
	}
	return nil
}

func (s GroupCredentialReconciliation) ReconcileGroup(ctx context.Context, groupID uint, now time.Time) error {
	if s.DB == nil || s.Issuer == nil {
		return entitlements.ErrCredentialReconciliationUnavailable
	}
	var subscriptions []model.Subscription
	if err := RunCredentialTransaction(ctx, s.DB, func(tx *gorm.DB) error {
		activeSubscriptionIDs := tx.Model(&model.Subscription{}).
			Select("id").
			Where("node_group_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total", groupID, "active", now)
		currentEndpointIDs := tx.Model(&model.NodeGroupEndpoint{}).
			Select("protocol_endpoint_id").
			Where("node_group_id = ?", groupID)
		if err := tx.Model(&model.ProtocolCredential{}).
			Where("subscription_id IN (?)", activeSubscriptionIDs).
			Where("protocol_endpoint_id NOT IN (?)", currentEndpointIDs).
			Where("status IN ? AND revoked_at IS NULL", []string{"active", "prepared"}).
			Updates(map[string]interface{}{"status": "revoked", "revoked_at": now}).Error; err != nil {
			return err
		}
		return tx.Where("node_group_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total", groupID, "active", now).
			Order("id asc").Find(&subscriptions).Error
	}); err != nil {
		return err
	}
	return s.ensureSubscriptions(ctx, subscriptions)
}

func (s GroupCredentialReconciliation) ensureSubscriptions(ctx context.Context, subscriptions []model.Subscription) error {
	ordered := append([]model.Subscription(nil), subscriptions...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	for _, subscription := range ordered {
		current, err := subscriptionCredentialsCurrent(s.DB.WithContext(ctx), subscription, s.Issuer)
		if err != nil {
			return err
		}
		if current {
			continue
		}
		err = RunCredentialTransaction(ctx, s.DB, func(tx *gorm.DB) error {
			_, err := EnsureCredentials(tx, subscription, s.Issuer)
			return err
		})
		if err == nil {
			continue
		}
		if errors.Is(err, ErrCredentialLockTimeout) {
			current, checkErr := subscriptionCredentialsCurrent(s.DB.WithContext(ctx), subscription, s.Issuer)
			if checkErr == nil && current {
				continue
			}
			if checkErr != nil {
				return errors.Join(err, checkErr)
			}
		}
		return err
	}
	return nil
}

func subscriptionCredentialsCurrent(db *gorm.DB, subscription model.Subscription, issuer entitlements.CredentialIssuer) (bool, error) {
	var endpoints []model.ProtocolEndpoint
	if err := db.Model(&model.ProtocolEndpoint{}).
		Joins("JOIN node_group_endpoints ON node_group_endpoints.protocol_endpoint_id = protocol_endpoints.id").
		Where("node_group_endpoints.node_group_id = ? AND protocol_endpoints.is_active = ?", subscription.NodeGroupID, true).
		Order("protocol_endpoints.sort_order asc, protocol_endpoints.id asc").
		Find(&endpoints).Error; err != nil {
		return false, err
	}
	endpointIDs := make([]uint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if issuer.Supports(endpoint.Protocol) {
			endpointIDs = append(endpointIDs, endpoint.ID)
		}
	}
	if len(endpointIDs) == 0 {
		return true, nil
	}
	var credentials []model.ProtocolCredential
	if err := db.Where("subscription_id = ? AND protocol_endpoint_id IN ?", subscription.ID, endpointIDs).Find(&credentials).Error; err != nil {
		return false, err
	}
	credentialsByEndpoint := make(map[uint]model.ProtocolCredential, len(credentials))
	for _, credential := range credentials {
		credentialsByEndpoint[credential.ProtocolEndpointID] = credential
	}
	for _, endpoint := range endpoints {
		if !issuer.Supports(endpoint.Protocol) {
			continue
		}
		credential, exists := credentialsByEndpoint[endpoint.ID]
		if !exists || credential.UserID != subscription.UserID || credential.NodeID != endpoint.NodeID ||
			credential.ListenPort != endpoint.Port || credential.PublicPort != endpoint.PublicPort ||
			credential.Status != issuer.Status(endpoint.Protocol, endpoint.MieruPrincipalReady) ||
			!credential.ExpiresAt.Equal(subscription.EndAt) || credential.RevokedAt != nil {
			return false, nil
		}
	}
	return true, nil
}
