package zero

import (
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

// RenderWithCredentialTargets returns evidence for the credential bindings
// actually rendered by this adapter, from the same immutable source snapshot.
// Every supported managed protocol either renders all supplied credentials or
// fails Render; fallback users are not subscription credential confirmations.
func (r RuntimeConfigurationRenderer) RenderWithCredentialTargets(request RuntimeConfigurationRenderRequest) ([]byte, string, network.ConfigurationDeliveryTargets, error) {
	payload, sha, err := r.Render(request)
	if err != nil {
		return nil, "", network.ConfigurationDeliveryTargets{}, err
	}
	targets := []network.CredentialDeliveryTarget{}
	for _, endpoint := range request.Snapshot.Endpoints {
		if endpoint.NodeID != request.NodeID {
			return nil, "", network.ConfigurationDeliveryTargets{}, fmt.Errorf("protocol endpoint %d belongs to another node", endpoint.ID)
		}
		for _, credential := range endpoint.Credentials {
			material := network.CredentialMaterialFingerprint(request.NodeID, endpoint.ID, credential.ID, credential.PrincipalKey, credential.Secret, credential.ListenPort, credential.PublicPort)
			targets = append(targets, network.CredentialDeliveryTarget{EndpointSHA256: network.CredentialEndpointFingerprint(endpoint), CredentialID: credential.ID, ProtocolEndpointID: endpoint.ID, MaterialSHA256: material, PolicyRevision: credential.PolicyRevision})
		}
	}
	routes := []network.NetworkEntryDeliveryTarget{}
	for _, entry := range request.Snapshot.NetworkEntries {
		if entry.NodeID != request.NodeID {
			return nil, "", network.ConfigurationDeliveryTargets{}, fmt.Errorf("network entry%d belongs to another node", entry.ID)
		}
		routes = append(routes, network.NetworkEntryDeliveryTarget{NetworkEntryID: entry.ID, MaterialSHA256: network.NetworkEntryDeliveryFingerprint(entry)})
	}
	if _, err := network.EncodeCredentialDeliveryManifest(targets, routes); err != nil {
		return nil, "", network.ConfigurationDeliveryTargets{}, err
	}
	return payload, sha, network.ConfigurationDeliveryTargets{Credentials: targets, NetworkEntries: routes}, nil
}
