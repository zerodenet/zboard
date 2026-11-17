package networkstore

import (
	"strconv"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Called only after exact node configuration confirmation. Repeated receipts
// never reset an existing deadline and settings changes affect future windows.
func confirmCredentialCompatibility(tx *gorm.DB, deployment model.ProtocolDeployment, manifest network.CredentialDeliveryManifest, now time.Time) error {
	hasRoot := false
	for _, target := range manifest.Targets {
		if target.MaterialVersion == 1 {
			hasRoot = true
			break
		}
	}
	if !hasRoot {
		return nil
	}
	days := 30
	var setting model.SystemConfig
	if e := tx.Where("config_key=?", "subscription_credential_compatibility_days").Limit(1).Find(&setting).Error; e != nil {
		return e
	}
	if setting.ID != 0 {
		v, e := strconv.Atoi(setting.Value)
		if e != nil || v < 1 || v > 365 {
			return entitlements.ErrCredentialMaterial
		}
		days = v
	}
	for _, target := range manifest.Targets {
		if target.MaterialVersion != 1 {
			continue
		}
		var root model.ProtocolCredential
		if e := tx.Where("id=? AND material_version=1 AND current_scope=1 AND status='active'", target.CredentialID).Limit(1).Find(&root).Error; e != nil {
			return e
		}
		if root.ID == 0 {
			continue
		}
		var ep model.ProtocolEndpoint
		if e := tx.Where("id=? AND node_id=?", target.ProtocolEndpointID, deployment.NodeID).Limit(1).Find(&ep).Error; e != nil {
			return e
		}
		if ep.ID == 0 {
			continue
		}
		material := network.CredentialMaterialFingerprint(ep.NodeID, ep.ID, root.ID, root.PrincipalKey, root.Secret, ep.Port, ep.PublicPort)
		endpointSHA := network.CredentialEndpointFingerprint(network.RuntimeConfigurationEndpoint{ID: ep.ID, NodeID: ep.NodeID, Protocol: ep.Protocol, Address: ep.Address, ListenAddress: ep.ListenAddress, Port: ep.Port, PublicPort: ep.PublicPort, ServerConfig: ep.ServerConfig, ClientConfig: ep.ClientConfig, EgressConfig: ep.EgressConfig})
		if material != target.MaterialSHA256 || endpointSHA != target.EndpointSHA256 {
			continue
		}
		until := now.AddDate(0, 0, days)
		if e := tx.Model(&model.ProtocolCredential{}).Where("subscription_id=? AND entitlement_epoch=? AND material_version=0 AND node_id=? AND protocol_endpoint_id=? AND status IN ? AND compatibility_started_at IS NULL", root.SubscriptionID, root.EntitlementEpoch, ep.NodeID, ep.ID, []string{"active", "prepared"}).Updates(map[string]any{"compatibility_started_at": now, "compatibility_until": until}).Error; e != nil {
			return e
		}
	}
	return nil
}
