package entitlementstore

import (
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type subscriptionMaterialIssuer interface {
	NewSubscriptionMaterial() (string, string, error)
	Decrypt(string) (string, error)
}

func prepareSubscriptionMaterial(tx *gorm.DB, sub model.Subscription, issuer entitlements.CredentialIssuer) (model.ProtocolCredential, error) {
	var root model.ProtocolCredential
	err := tx.Where("subscription_id=? AND current_scope=1 AND material_scope=1", sub.ID).First(&root).Error
	if err == nil {
		return root, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return root, err
	}
	material, ok := issuer.(subscriptionMaterialIssuer)
	if !ok {
		return root, entitlements.ErrCredentialMaterial
	}
	secret, verifier, err := material.NewSubscriptionMaterial()
	if err != nil {
		return root, err
	}
	fingerprint := sha256.Sum256([]byte(verifier))
	scope := uint8(1)
	root = model.ProtocolCredential{MaterialGroupID: sub.NodeGroupID, SubscriptionID: sub.ID, UserID: sub.UserID, EntitlementEpoch: sub.EntitlementEpoch, CredentialID: fmt.Sprintf("subscription-material-%x", fingerprint[:16]), PrincipalKey: fmt.Sprintf("subscription-material:%x", fingerprint[:16]), Secret: secret, Status: "active", ExpiresAt: sub.EndAt, DeliveryRequired: true, MaterialVersion: entitlements.CredentialMaterialVersion, MaterialVerifier: verifier, MaterialScope: &scope, PrincipalLookupVersion: 1}
	// NULL parents deliberately distinguish one subscription root from its
	// transient endpoint projections. No placeholder node or endpoint is created.
	if err := tx.Omit("NodeID", "ProtocolEndpointID").Create(&root).Error; err != nil {
		return root, err
	}
	if err := tx.Model(&model.Subscription{}).Where("id=?", sub.ID).UpdateColumn("credential_material_mode", 1).Error; err != nil {
		return root, err
	}
	return root, nil
}

func materialCredentialBindings(root model.ProtocolCredential, endpoints []model.ProtocolEndpoint, issuer entitlements.CredentialIssuer) ([]model.ProtocolCredential, error) {
	cipher, ok := issuer.(subscriptionMaterialIssuer)
	if !ok {
		return nil, entitlements.ErrCredentialMaterial
	}
	rows := make([]model.ProtocolCredential, 0, len(endpoints))
	for _, ep := range endpoints {
		if !issuer.Supports(ep.Protocol) {
			continue
		}
		row, err := credentialstore.BindMaterial(root, ep, cipher)
		if err != nil {
			return nil, err
		}
		row.Status = issuer.Status(ep.Protocol, ep.MieruPrincipalReady)
		rows = append(rows, row)
	}
	return rows, nil
}

func reconcileMaterialGroup(tx *gorm.DB, root *model.ProtocolCredential, sub model.Subscription) error {
	if root.MaterialGroupID == sub.NodeGroupID {
		return nil
	}
	if root.MaterialGroupID != 0 {
		old, e := networkstore.LockCurrentCredentialEndpointRoutes(tx, root.MaterialGroupID)
		if e != nil {
			return e
		}
		seen := map[uint]bool{}
		for _, route := range old {
			if seen[route.NodeID] {
				continue
			}
			seen[route.NodeID] = true
			if e := networkstore.EnqueuePublication(tx, route.NodeID, 0, 0); e != nil {
				return e
			}
		}
	}
	if e := tx.Model(&model.ProtocolCredential{}).Where("id=?", root.ID).UpdateColumn("material_group_id", sub.NodeGroupID).Error; e != nil {
		return e
	}
	root.MaterialGroupID = sub.NodeGroupID
	return nil
}
