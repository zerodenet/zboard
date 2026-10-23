package entitlementstore

import (
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func EnsureCredentials(tx *gorm.DB, subscription model.Subscription, issuer entitlements.CredentialIssuer) ([]model.ProtocolCredential, error) {
	if _, transactional := tx.Statement.ConnPool.(gorm.TxCommitter); !transactional {
		var result []model.ProtocolCredential
		err := tx.Transaction(func(inner *gorm.DB) error {
			var err error
			result, err = EnsureCredentials(inner, subscription, issuer)
			return err
		})
		return result, err
	}
	// Reconciliation jobs may carry a snapshot taken before retirement, renewal
	// or plan change. Only the locked current business state can grant access.
	var current model.Subscription
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, subscription.ID).Error; err != nil {
		return nil, err
	}
	if current.Status != "active" || current.FlowUsed >= current.FlowTotal {
		return nil, nil
	}
	subscription = current
	endpoints, err := networkstore.CurrentCredentialEndpoints(tx, subscription.NodeGroupID)
	if err != nil {
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
			Where("subscription_id = ? AND protocol_endpoint_id = ? AND current_scope = ?", subscription.ID, endpoint.ID, 1).
			First(&credential).Error
		if err == nil {
			if credential.EntitlementEpoch != subscription.EntitlementEpoch {
				return nil, errors.New("credential entitlement epoch mismatch")
			}
			updates := map[string]interface{}{
				"user_id":     subscription.UserID,
				"node_id":     endpoint.NodeID,
				"listen_port": endpoint.Port,
				"public_port": endpoint.PublicPort,
				"status":      targetStatus,
				"expires_at":  subscription.EndAt,
				"revoked_at":  nil,
			}
			if credential.NodeID != endpoint.NodeID {
				updates["principal_lookup_version"] = 0
				updates["principal_digest"] = ""
				updates["secret_principal_digest"] = ""
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
		credentialID := fmt.Sprintf("subscription-%d-endpoint-%d", subscription.ID, endpoint.ID)
		if subscription.EntitlementEpoch > 0 {
			credentialID += fmt.Sprintf("-epoch-%d", subscription.EntitlementEpoch)
		}
		credential = model.ProtocolCredential{
			EntitlementEpoch:   subscription.EntitlementEpoch,
			SubscriptionID:     subscription.ID,
			UserID:             subscription.UserID,
			ProtocolEndpointID: endpoint.ID,
			NodeID:             endpoint.NodeID,
			CredentialID:       credentialID,
			PrincipalKey:       fmt.Sprintf("subscription:%d:endpoint:%d", subscription.ID, endpoint.ID),
			Secret:             encryptedSecret,
			ListenPort:         listenPort,
			PublicPort:         publicPort,
			Status:             targetStatus,
			ExpiresAt:          subscription.EndAt,
		}
		credential.DeliveryRequired = true
		credential.SecretPrincipalDigest = entitlements.RetiredPrincipalHash(endpoint.NodeID, secret)
		credential.PrincipalLookupVersion = 1
		if err := tx.Create(&credential).Error; err != nil {
			return nil, err
		}
		credentials = append(credentials, credential)
	}
	return credentials, nil
}
