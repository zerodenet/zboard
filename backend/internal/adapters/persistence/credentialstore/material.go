package credentialstore

import (
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type materialDecryptor interface{ Decrypt(string) (string, error) }

// BindMaterial constructs a render/attribution projection. It never inserts a
// credential relation. The root ID is the one stable financial parent.
func BindMaterial(root model.ProtocolCredential, endpoint model.ProtocolEndpoint, cipher materialDecryptor) (model.ProtocolCredential, error) {
	if root.MaterialVersion != entitlements.CredentialMaterialVersion || cipher == nil {
		return model.ProtocolCredential{}, entitlements.ErrCredentialMaterial
	}
	seed, err := cipher.Decrypt(root.Secret)
	if err != nil {
		return model.ProtocolCredential{}, err
	}
	principal, err := entitlements.CredentialBindingPrincipal(seed, entitlements.CredentialBinding{CredentialID: root.ID, NodeID: endpoint.NodeID, EndpointID: endpoint.ID})
	if err != nil {
		return model.ProtocolCredential{}, err
	}
	root.NodeID, root.ProtocolEndpointID = endpoint.NodeID, endpoint.ID
	root.ListenPort, root.PublicPort = endpoint.Port, endpoint.PublicPort
	root.PrincipalKey = principal
	return root, nil
}

func lookupMaterialBinding(db *gorm.DB, node uint, principal string) (model.ProtocolCredential, error) {
	binding, err := entitlements.ParseCredentialBinding(principal)
	if err != nil || binding.NodeID != node {
		return model.ProtocolCredential{}, ErrPrincipalIndexPending
	}
	var root model.ProtocolCredential
	if err = db.First(&root, binding.CredentialID).Error; err != nil {
		return root, err
	}
	if root.MaterialVersion != entitlements.CredentialMaterialVersion {
		return model.ProtocolCredential{}, ErrPrincipalIndexPending
	}
	if err = VerifyMaterialBinding(root, node, principal); err != nil {
		return model.ProtocolCredential{}, fmt.Errorf("%w: signed binding verification failed", ErrPrincipalIndexPending)
	}
	var endpoint model.ProtocolEndpoint
	if err = db.Unscoped().First(&endpoint, binding.EndpointID).Error; err != nil {
		return model.ProtocolCredential{}, err
	}
	// Attribution follows the signed historical node identity, rather than a
	// mutable group membership or an endpoint's new placement after migration.
	root.NodeID, root.ProtocolEndpointID = node, binding.EndpointID
	root.ListenPort, root.PublicPort = endpoint.Port, endpoint.PublicPort
	root.PrincipalKey = principal
	return root, nil
}

func VerifyMaterialBinding(root model.ProtocolCredential, node uint, principal string) error {
	binding, err := entitlements.VerifyCredentialBinding(root.MaterialVerifier, principal)
	if err != nil || binding.CredentialID != root.ID || binding.NodeID != node || (root.ProtocolEndpointID != 0 && binding.EndpointID != root.ProtocolEndpointID) {
		return errors.New("subscription credential attribution does not match its signed binding")
	}
	return nil
}
