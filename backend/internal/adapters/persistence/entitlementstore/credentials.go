package entitlementstore

import (
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func EnsureCredentials(tx *gorm.DB, subscription model.Subscription, issuer entitlements.CredentialIssuer) ([]model.ProtocolCredential, error) {
	var endpoints []model.ProtocolEndpoint
	if err := tx.Model(&model.ProtocolEndpoint{}).
		Joins("JOIN node_group_endpoints ON node_group_endpoints.protocol_endpoint_id = protocol_endpoints.id").
		Where("node_group_endpoints.node_group_id = ? AND protocol_endpoints.is_active = ?", subscription.NodeGroupID, true).
		Order("protocol_endpoints.sort_order asc, protocol_endpoints.id asc").
		Find(&endpoints).Error; err != nil {
		return nil, err
	}

	credentials := make([]model.ProtocolCredential, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if !issuer.Supports(endpoint.Protocol) {
			continue
		}
		targetStatus := issuer.Status(endpoint.Protocol, endpoint.MieruPrincipalReady)
		var credential model.ProtocolCredential
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("subscription_id = ? AND protocol_endpoint_id = ?", subscription.ID, endpoint.ID).
			First(&credential).Error
		if err == nil {
			updates := map[string]interface{}{
				"user_id":     subscription.UserID,
				"node_id":     endpoint.NodeID,
				"listen_port": endpoint.Port,
				"public_port": endpoint.PublicPort,
				"status":      targetStatus,
				"expires_at":  subscription.EndAt,
				"revoked_at":  nil,
			}
			if err := tx.Model(&credential).Updates(updates).Error; err != nil {
				return nil, err
			}
			credential.UserID = subscription.UserID
			credential.NodeID = endpoint.NodeID
			credential.ListenPort = endpoint.Port
			credential.PublicPort = endpoint.PublicPort
			credential.Status = targetStatus
			credential.ExpiresAt = subscription.EndAt
			credential.RevokedAt = nil
			credentials = append(credentials, credential)
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}

		secret, err := issuer.Secret(endpoint.Protocol, endpoint.ServerConfig)
		if err != nil {
			return nil, err
		}
		encryptedSecret, err := issuer.Encrypt(secret)
		if err != nil {
			return nil, err
		}
		listenPort, publicPort := endpoint.Port, endpoint.PublicPort
		credential = model.ProtocolCredential{
			SubscriptionID:     subscription.ID,
			UserID:             subscription.UserID,
			ProtocolEndpointID: endpoint.ID,
			NodeID:             endpoint.NodeID,
			CredentialID:       fmt.Sprintf("subscription-%d-endpoint-%d", subscription.ID, endpoint.ID),
			PrincipalKey:       fmt.Sprintf("subscription:%d:endpoint:%d", subscription.ID, endpoint.ID),
			Secret:             encryptedSecret,
			ListenPort:         listenPort,
			PublicPort:         publicPort,
			Status:             targetStatus,
			ExpiresAt:          subscription.EndAt,
		}
		if err := tx.Create(&credential).Error; err != nil {
			return nil, err
		}
		credentials = append(credentials, credential)
	}
	return credentials, nil
}
