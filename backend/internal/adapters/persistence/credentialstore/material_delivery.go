package credentialstore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"gorm.io/gorm"
)

// Read each node's immutable source once, rather than joining its full manifest
// into every subscription x endpoint result and decoding it once per binding.
// Nothing is persisted or cached beyond this bounded projection read.
func ConfirmMaterials(db *gorm.DB, rows []DeliveryProjection) error {
	nodes := []uint{}
	seen := map[uint]bool{}
	for _, r := range rows {
		if r.MaterialVersion == 1 && !seen[r.NodeID] {
			seen[r.NodeID] = true
			nodes = append(nodes, r.NodeID)
		}
	}
	type source struct {
		NodeID        uint
		SHA, Manifest string
	}
	type key struct{ node, credential, endpoint uint }
	type proof struct {
		sha    string
		target network.CredentialDeliveryTarget
	}
	wanted := map[key]bool{}
	for _, r := range rows {
		if r.MaterialVersion == 1 {
			wanted[key{r.NodeID, r.ID, r.ProtocolEndpointID}] = true
		}
	}
	proofs := map[key]proof{}
	for offset := 0; offset < len(nodes); offset += 128 {
		var sources []source
		if e := db.Table("node_credential_delivery_states AS state").Select("state.node_id,state.config_sha256 AS sha,source.credential_targets AS manifest").Joins("JOIN protocol_deployments AS source ON source.id=state.deployment_id AND source.node_id=state.node_id AND source.status='succeeded' AND source.applied_config_sha256=state.config_sha256 AND source.desired_config_sha256=state.config_sha256").Where("state.node_id IN ?", nodes[offset:min(offset+128, len(nodes))]).Scan(&sources).Error; e != nil {
			return e
		}
		for _, s := range sources {
			manifest, e := network.DecodeConfigurationDeliveryManifest(s.Manifest)
			if e != nil {
				continue
			}
			for _, target := range manifest.Targets {
				if target.MaterialVersion == 1 && wanted[key{s.NodeID, target.CredentialID, target.ProtocolEndpointID}] {
					proofs[key{s.NodeID, target.CredentialID, target.ProtocolEndpointID}] = proof{s.SHA, target}
				}
			}
		}
	}
	for i := range rows {
		p := &rows[i]
		if p.MaterialVersion != 1 || p.TrackingNodeID == nil || len(p.TrackingSHA) != 64 {
			continue
		}
		proof, ok := proofs[key{p.NodeID, p.ID, p.ProtocolEndpointID}]
		if !ok || proof.sha != p.TrackingSHA {
			continue
		}
		material := network.CredentialMaterialFingerprint(p.NodeID, p.ProtocolEndpointID, p.ID, p.PrincipalKey, p.Secret, p.ListenPort, p.PublicPort)
		endpoint := network.CredentialEndpointFingerprint(network.RuntimeConfigurationEndpoint{ID: p.ProtocolEndpointID, NodeID: p.BindingNodeID, Protocol: p.BindingProtocol, Address: p.BindingAddress, ListenAddress: p.BindingListenAddress, Port: p.BindingPort, PublicPort: p.BindingPublicPort, ServerConfig: p.BindingServerConfig, ClientConfig: p.BindingClientConfig, EgressConfig: p.BindingEgressConfig})
		p.materialConfirmed = material != "" && material == proof.target.MaterialSHA256 && endpoint == proof.target.EndpointSHA256
	}
	return nil
}
